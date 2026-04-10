package financialtruth

// NormalizeEvent converts a FinancialEvent into a FinancialFact deterministically.
//
// Rules:
//   - inflow + non-transfer → income fact
//   - outflow + non-transfer → expense fact
//   - transfer_in / transfer_out → transfer fact (not income by default)
//   - confidence reflects source quality
//   - verified = true only for bank or manual sources
func NormalizeEvent(e FinancialEvent) FinancialFact {
	f := FinancialFact{
		Amount:     e.Amount,
		Currency:   e.Currency,
		Source:     e.Source,
		EventID:    e.ID,
		OccurredAt: e.OccurredAt,
	}

	// Determine fact type based on event type + direction.
	if transferEventTypes[e.EventType] {
		f.FactType = FactTypeTransfer
	} else if e.Direction == DirectionInflow {
		f.FactType = FactTypeIncome
	} else if e.Direction == DirectionOutflow {
		f.FactType = FactTypeExpense
	} else {
		// Fallback: use direction as hint.
		f.FactType = FactTypeTransfer
	}

	// Assign confidence based on source quality.
	f.Confidence = confidenceForSource(e.Source)

	// Verified only for trusted sources.
	f.Verified = isVerifiedSource(e.Source)

	// FinanciallyVerified requires both verification and a link (set later).
	f.FinanciallyVerified = false

	return f
}

// confidenceForSource assigns a deterministic confidence based on data source.
func confidenceForSource(source string) float64 {
	switch source {
	case "bank":
		return VerifiedConfidence
	case "manual":
		return ManualSourceConfidence
	case "system":
		return SystemSourceConfidence
	case "invoice":
		return ManualSourceConfidence
	case "external":
		return ExternalSourceConfidence
	default:
		return ExternalSourceConfidence
	}
}

// isVerifiedSource returns true for sources considered ground truth.
func isVerifiedSource(source string) bool {
	switch source {
	case "bank", "manual", "invoice":
		return true
	default:
		return false
	}
}
