package telegram

import (
	"fmt"
	"html"
	"unicode/utf8"
)

const (
	maxTitleLen      = 120
	maxSuggestionLen = 200
	maxReasonLen     = 280
)

// ProposalDetails holds all display data for a proposal notification message.
type ProposalDetails struct {
	ProposalID   string
	TaskTitle    string  // full source task title
	ProposalType string  // e.g. "rewrite", "routing"
	Suggestion   string  // rewritten title or suggested list name
	Confidence   float64 // 0 means not available
	Reason       string  // LLM reasoning
}

// FormatProposal returns an HTML-formatted Telegram message for a new proposal.
// All user-supplied text is HTML-escaped and safely truncated.
//
// Example output:
//
//	🧠 New Proposal
//
//	Task:
//	Buy groceries and pick up dry cleaning
//
//	Type:
//	rewrite
//
//	Suggestion:
//	Grocery run + dry cleaning pickup
//
//	Confidence:
//	92%
//
//	Reason:
//	The original title lists two tasks; combining them is clearer.
func FormatProposal(d ProposalDetails) string {
	title := safeField(d.TaskTitle, maxTitleLen, "(no title)")
	pType := safeField(d.ProposalType, 80, "(unknown)")
	suggestion := safeField(d.Suggestion, maxSuggestionLen, "—")
	reason := safeField(d.Reason, maxReasonLen, "—")

	confidence := "—"
	if d.Confidence > 0 {
		confidence = fmt.Sprintf("%.0f%%", d.Confidence*100)
	}

	return fmt.Sprintf(
		"🧠 <b>New Proposal</b>\n\n"+
			"<b>Task:</b>\n%s\n\n"+
			"<b>Type:</b>\n%s\n\n"+
			"<b>Suggestion:</b>\n%s\n\n"+
			"<b>Confidence:</b>\n%s\n\n"+
			"<b>Reason:</b>\n%s",
		title, pType, suggestion, confidence, reason,
	)
}

// safeField HTML-escapes and truncates s, returning fallback when s is empty.
func safeField(s string, maxLen int, fallback string) string {
	if s == "" {
		return fallback
	}
	return html.EscapeString(truncateRunes(s, maxLen))
}

// truncateRunes truncates s to at most maxLen Unicode code points,
// appending an ellipsis when truncation occurs.
func truncateRunes(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "…"
}
