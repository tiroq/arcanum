package financialtruth

import (
	"context"
	"time"
)

// ComputeSummary computes a monthly financial truth summary from the fact store.
// month is in "2006-01" format. Fail-open: returns zero summary on error.
func ComputeSummary(ctx context.Context, factStore *FactStore, month string) FinancialSummary {
	summary := FinancialSummary{
		Month:     month,
		UpdatedAt: time.Now().UTC(),
	}

	verifiedIncome, verifiedExpenses, err := factStore.SumVerifiedByMonth(ctx, month)
	if err != nil {
		return summary
	}
	summary.CurrentMonthIncomeVerified = verifiedIncome
	summary.CurrentMonthExpensesVerified = verifiedExpenses
	summary.CurrentMonthNetVerified = verifiedIncome - verifiedExpenses

	unverifiedInflow, unverifiedOutflow, err := factStore.SumUnverifiedByMonth(ctx, month)
	if err != nil {
		return summary
	}
	summary.PendingUnverifiedInflow = unverifiedInflow
	summary.PendingUnverifiedOutflow = unverifiedOutflow

	total, verified, err := factStore.CountByMonth(ctx, month)
	if err != nil {
		return summary
	}
	summary.TotalFacts = total
	summary.VerifiedFacts = verified

	return summary
}

// CurrentMonth returns the current month string in "2006-01" format.
func CurrentMonth() string {
	return time.Now().UTC().Format("2006-01")
}
