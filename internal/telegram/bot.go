package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Bot wraps the Telegram Bot API for owner communication.
type Bot struct {
	api         *tgbotapi.BotAPI
	ownerChatID int64
	pool        *pgxpool.Pool
	logger      *zap.Logger
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// New creates a new Telegram bot. Returns error if token or chatID are not configured.
func New(token string, ownerChatID int64, pool *pgxpool.Pool, logger *zap.Logger) (*Bot, error) {
	if token == "" || ownerChatID == 0 {
		return nil, fmt.Errorf("telegram bot token and owner chat ID are required")
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	logger.Info("telegram bot authorized",
		zap.String("username", api.Self.UserName),
		zap.Int64("owner_chat_id", ownerChatID),
	)

	return &Bot{
		api:         api,
		ownerChatID: ownerChatID,
		pool:        pool,
		logger:      logger,
	}, nil
}

// SendMessage sends a text message to the owner.
func (b *Bot) SendMessage(text string) error {
	msg := tgbotapi.NewMessage(b.ownerChatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true

	_, err := b.api.Send(msg)
	if err != nil {
		b.logger.Error("failed to send telegram message",
			zap.Error(err),
			zap.Int("text_len", len(text)),
		)
		return fmt.Errorf("send telegram message: %w", err)
	}
	return nil
}

// StartPolling begins long-polling for incoming owner commands.
func (b *Bot) StartPolling(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := b.api.GetUpdatesChan(u)

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-updates:
				if !ok {
					return
				}
				b.handleUpdate(update)
			}
		}
	}()
	b.logger.Info("telegram polling started")
}

// Stop shuts down the bot polling gracefully.
func (b *Bot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.api.StopReceivingUpdates()
	b.wg.Wait()
	b.logger.Info("telegram bot stopped")
}

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	// Security: only respond to the configured owner
	if update.Message.Chat.ID != b.ownerChatID {
		b.logger.Warn("ignoring message from unauthorized chat",
			zap.Int64("chat_id", update.Message.Chat.ID),
			zap.String("username", update.Message.From.UserName),
		)
		return
	}

	if !update.Message.IsCommand() {
		return
	}

	var reply string
	switch update.Message.Command() {
	case "start":
		reply = "Arcanum online. Use /help to see available commands."
	case "help":
		reply = b.handleHelp()
	case "status":
		reply = b.handleStatus()
	case "queue":
		b.handleQueue()
	case "models":
		reply = b.handleModels()
	default:
		cmd := update.Message.Command()
		if strings.HasPrefix(cmd, "approve_") {
			reply = b.handleApprove(strings.TrimPrefix(cmd, "approve_"))
		} else if strings.HasPrefix(cmd, "reject_") {
			reply = b.handleReject(strings.TrimPrefix(cmd, "reject_"))
		} else {
			reply = fmt.Sprintf("Unknown command: /%s\nUse /help to see available commands.", cmd)
		}
	}

	if reply != "" {
		if err := b.SendMessage(reply); err != nil {
			b.logger.Error("failed to send reply", zap.Error(err))
		}
	}
}

func (b *Bot) handleHelp() string {
	return `<b>Arcanum Commands</b>

/status — System state: services, queue depth, recent errors
/queue — Pending proposals (with Approve/Reject buttons)
/models — Ollama model status
/help — This message

<b>Proposal Actions</b>
Use the <b>✅ Approve</b> / <b>❌ Reject</b> buttons on proposal notifications.
Manual (debug): /approve_&lt;uuid&gt; or /reject_&lt;uuid&gt;`
}

func (b *Bot) handleStatus() string {
	ctx := context.Background()

	var jobCounts struct {
		Queued    int
		Running   int
		Succeeded int
		Failed    int
	}

	rows, err := b.pool.Query(ctx, `
SELECT status, COUNT(*) FROM processing_jobs
WHERE created_at > NOW() - INTERVAL '24 hours'
GROUP BY status`)
	if err != nil {
		return fmt.Sprintf("Error querying jobs: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			continue
		}
		switch status {
		case "queued":
			jobCounts.Queued = count
		case "running", "leased":
			jobCounts.Running += count
		case "succeeded":
			jobCounts.Succeeded = count
		case "failed", "dead_letter":
			jobCounts.Failed += count
		}
	}

	var pendingProposals int
	_ = b.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM suggestion_proposals WHERE approval_status = 'pending'`).Scan(&pendingProposals)

	return fmt.Sprintf(`<b>Arcanum Status</b> (last 24h)

<b>Jobs:</b> %d queued, %d running, %d succeeded, %d failed
<b>Proposals:</b> %d pending review`,
		jobCounts.Queued, jobCounts.Running, jobCounts.Succeeded, jobCounts.Failed,
		pendingProposals,
	)
}

func (b *Bot) handleQueue() {
	ctx := context.Background()

	rows, err := b.pool.Query(ctx, `
SELECT p.id, p.proposal_type, COALESCE(t.title, 'untitled')
FROM suggestion_proposals p
LEFT JOIN source_tasks t ON p.source_task_id = t.id
WHERE p.approval_status = 'pending'
ORDER BY p.created_at DESC
LIMIT 10`)
	if err != nil {
		b.logger.Error("failed to query pending proposals", zap.Error(err))
		b.SendMessage("Error querying proposals.") //nolint:errcheck
		return
	}
	defer rows.Close()

	type proposal struct{ id, pType, title string }
	var proposals []proposal
	for rows.Next() {
		var p proposal
		if err := rows.Scan(&p.id, &p.pType, &p.title); err != nil {
			continue
		}
		proposals = append(proposals, p)
	}

	if len(proposals) == 0 {
		b.SendMessage("No pending proposals.") //nolint:errcheck
		return
	}

	var lines []string
	var kbRows [][]tgbotapi.InlineKeyboardButton
	for _, p := range proposals {
		short := shortID(p.id)
		lines = append(lines, fmt.Sprintf("• <b>%s</b> [%s] %s", short, p.pType, p.title))
		kbRows = append(kbRows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ "+short, "approve:"+p.id),
			tgbotapi.NewInlineKeyboardButtonData("❌ "+short, "reject:"+p.id),
		))
	}

	msg := tgbotapi.NewMessage(b.ownerChatID, "<b>Pending Proposals</b>\n\n"+strings.Join(lines, "\n"))
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(kbRows...)
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Error("failed to send queue message", zap.Error(err))
	}
}

func (b *Bot) handleModels() string {
	return `<b>Ollama Model Roles</b>

<b>default:</b> qwen3:1.7b (think=off)
<b>fast:</b> qwen3:1.7b → qwen3.5:0.8b
<b>planner:</b> qwen3:8b (think=on) → fallbacks
<b>review:</b> qwen3:1.7b (json=true) → fallbacks

Use /status for system health.`
}

func (b *Bot) handleApprove(proposalID string) string {
	ctx := context.Background()

	tag, err := b.pool.Exec(ctx, `
UPDATE suggestion_proposals
SET approval_status = 'approved', approved_by = 'telegram_owner', auto_approved = false, reviewed_at = NOW(), updated_at = NOW()
WHERE id = $1 AND approval_status = 'pending'`, proposalID)
	if err != nil {
		b.logger.Error("db error approving proposal",
			zap.String("proposal_id", proposalID),
			zap.Error(err),
		)
		return "DB error: could not approve proposal."
	}
	short := shortID(proposalID)
	if tag.RowsAffected() == 0 {
		return fmt.Sprintf("Proposal %s not found or already reviewed.", short)
	}
	return fmt.Sprintf("✅ Proposal %s approved.", short)
}

func (b *Bot) handleReject(proposalID string) string {
	ctx := context.Background()

	tag, err := b.pool.Exec(ctx, `
UPDATE suggestion_proposals
SET approval_status = 'rejected', approved_by = 'telegram_owner', reviewed_at = NOW(), updated_at = NOW()
WHERE id = $1 AND approval_status = 'pending'`, proposalID)
	if err != nil {
		b.logger.Error("db error rejecting proposal",
			zap.String("proposal_id", proposalID),
			zap.Error(err),
		)
		return "DB error: could not reject proposal."
	}
	short := shortID(proposalID)
	if tag.RowsAffected() == 0 {
		return fmt.Sprintf("Proposal %s not found or already reviewed.", short)
	}
	return fmt.Sprintf("❌ Proposal %s rejected.", short)
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// SendProposalMessage queries the DB for proposal details and sends a rich
// notification with Approve / Reject inline keyboard buttons.
func (b *Bot) SendProposalMessage(proposalID, sourceTaskID, proposalType string) error {
	ctx := context.Background()

	var title string
	var payloadBytes []byte
	if err := b.pool.QueryRow(ctx, `
SELECT COALESCE(t.title, ''), p.proposal_payload
FROM suggestion_proposals p
LEFT JOIN source_tasks t ON p.source_task_id = t.id
WHERE p.id = $1`, proposalID).Scan(&title, &payloadBytes); err != nil {
		b.logger.Warn("SendProposalMessage: db lookup failed",
			zap.String("proposal_id", proposalID),
			zap.Error(err),
		)
	}

	var payload map[string]interface{}
	_ = json.Unmarshal(payloadBytes, &payload)

	details := ProposalDetails{
		ProposalID:   proposalID,
		TaskTitle:    title,
		ProposalType: proposalType,
	}
	if payload != nil {
		// rewrite produces rewritten_title; routing produces suggested_list.
		if v, ok := payload["rewritten_title"].(string); ok {
			details.Suggestion = v
		} else if v, ok := payload["suggested_list"].(string); ok {
			details.Suggestion = v
		}
		if v, ok := payload["confidence"].(float64); ok {
			details.Confidence = v
		}
		if v, ok := payload["reasoning"].(string); ok {
			details.Reason = v
		}
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Approve", "approve:"+proposalID),
			tgbotapi.NewInlineKeyboardButtonData("❌ Reject", "reject:"+proposalID),
		),
	)

	msg := tgbotapi.NewMessage(b.ownerChatID, FormatProposal(details))
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = keyboard

	if _, err := b.api.Send(msg); err != nil {
		b.logger.Error("failed to send proposal message", zap.Error(err))
		return fmt.Errorf("send proposal message: %w", err)
	}
	return nil
}

// handleCallbackQuery processes inline keyboard button presses.
func (b *Bot) handleCallbackQuery(query *tgbotapi.CallbackQuery) {
	if query.From == nil || query.From.ID != b.ownerChatID {
		b.logger.Warn("ignoring callback from unauthorized user")
		return
	}

	var reply string
	switch {
	case strings.HasPrefix(query.Data, "approve:"):
		reply = b.handleApprove(strings.TrimPrefix(query.Data, "approve:"))
	case strings.HasPrefix(query.Data, "reject:"):
		reply = b.handleReject(strings.TrimPrefix(query.Data, "reject:"))
	}

	// Acknowledge the callback so Telegram removes the loading indicator.
	cb := tgbotapi.NewCallback(query.ID, reply)
	if _, err := b.api.Request(cb); err != nil {
		b.logger.Warn("failed to answer callback query", zap.Error(err))
	}
}

// formatProposalText returns the HTML body for a proposal notification (no action buttons).
func formatProposalText(proposalID, sourceTaskID, proposalType string, humanReview bool) string {
	review := ""
	if humanReview {
		review = "\n⚠️ Requires human review."
	}
	taskShort := shortID(sourceTaskID)
	return fmt.Sprintf(`<b>New Proposal</b>

Type: %s
Task: <code>%s</code>
ID: <code>%s</code>%s`, proposalType, taskShort, proposalID, review)
}

// FormatProposalCreated formats a ProposalCreatedEvent for Telegram delivery.
// Deprecated: use Bot.SendProposalMessage to include inline action buttons.
func FormatProposalCreated(proposalID, sourceTaskID, proposalType string, humanReview bool) string {
	return formatProposalText(proposalID, sourceTaskID, proposalType, humanReview)
}

// FormatJobDead formats a JobDeadEvent for Telegram delivery.
func FormatJobDead(jobID, reason string) string {
	return fmt.Sprintf("<b>Job Dead-Lettered</b>\n\nJob: <code>%s</code>\nReason: %s", jobID[:8], reason)
}

// FormatJobFailed formats a job failure for Telegram delivery.
func FormatJobFailed(jobID, reason string, attempt int) string {
	return fmt.Sprintf("<b>Job Failed</b>\n\nJob: <code>%s</code>\nAttempt: %d\nReason: %s", jobID[:8], attempt, reason)
}

// FormatControlAlertLeaseExpired formats a lease-expiry reclaim alert.
func FormatControlAlertLeaseExpired(count int64) string {
	return fmt.Sprintf("<b>Control alert: expired leases reclaimed</b>\n\nCount: %d", count)
}

// FormatControlAlertRetryOverdue formats a retry-overdue requeue alert.
func FormatControlAlertRetryOverdue(count int64) string {
	return fmt.Sprintf("<b>Control alert: overdue retries requeued</b>\n\nCount: %d", count)
}

// FormatControlAlertQueueBacklog formats a queue-backlog alert.
func FormatControlAlertQueueBacklog(count, threshold int64) string {
	return fmt.Sprintf("<b>Control alert: queue backlog</b>\n\nQueued: %d (threshold: %d)", count, threshold)
}

// FormatControlAlertLeaseLost formats a mid-execution lease-loss alert.
func FormatControlAlertLeaseLost(jobID, workerID string) string {
	short := jobID
	if len(jobID) > 8 {
		short = jobID[:8]
	}
	return fmt.Sprintf("<b>Control alert: lease lost during execution</b>\n\nJob: <code>%s</code>\nWorker: <code>%s</code>", short, workerID)
}

// ParseEventJSON is a helper to unmarshal NATS message data into a map.
func ParseEventJSON(data []byte) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
