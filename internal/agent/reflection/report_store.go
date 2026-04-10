package reflection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReportStore persists and retrieves meta-reflection reports.
type ReportStore struct {
	db *pgxpool.Pool
}

// NewReportStore creates a ReportStore backed by PostgreSQL.
func NewReportStore(db *pgxpool.Pool) *ReportStore {
	return &ReportStore{db: db}
}

// SaveReport persists a MetaReflectionReport as JSON.
func (s *ReportStore) SaveReport(ctx context.Context, report MetaReflectionReport) error {
	jsonData, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	const q = `
INSERT INTO agent_reflection_reports (id, period_start, period_end, json_data, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET json_data = EXCLUDED.json_data`

	_, err = s.db.Exec(ctx, q, report.ID, report.PeriodStart, report.PeriodEnd, jsonData, report.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert report: %w", err)
	}
	return nil
}

// ListReports retrieves the most recent reports, ordered by created_at DESC.
func (s *ReportStore) ListReports(ctx context.Context, limit int) ([]MetaReflectionReport, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}

	const q = `
SELECT id, period_start, period_end, json_data, created_at
FROM agent_reflection_reports
ORDER BY created_at DESC
LIMIT $1`

	rows, err := s.db.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("query reports: %w", err)
	}
	defer rows.Close()

	var results []MetaReflectionReport
	for rows.Next() {
		var id string
		var periodStart, periodEnd, createdAt time.Time
		var jsonData []byte

		if err := rows.Scan(&id, &periodStart, &periodEnd, &jsonData, &createdAt); err != nil {
			return nil, fmt.Errorf("scan report: %w", err)
		}

		var report MetaReflectionReport
		if err := json.Unmarshal(jsonData, &report); err != nil {
			// Fallback: return skeleton with metadata
			report = MetaReflectionReport{
				ID:          id,
				PeriodStart: periodStart,
				PeriodEnd:   periodEnd,
				CreatedAt:   createdAt,
			}
		}
		// Ensure metadata matches DB columns
		report.ID = id
		report.PeriodStart = periodStart
		report.PeriodEnd = periodEnd
		report.CreatedAt = createdAt
		results = append(results, report)
	}
	return results, rows.Err()
}

// GetLatest retrieves the single most recent report, or nil if none exist.
func (s *ReportStore) GetLatest(ctx context.Context) (*MetaReflectionReport, error) {
	reports, err := s.ListReports(ctx, 1)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, nil
	}
	return &reports[0], nil
}
