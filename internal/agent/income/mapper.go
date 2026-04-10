package income

// incomeActionTypes is the set of agent action type strings that are considered
// income-oriented. Used by the graph adapter to identify which paths should
// receive the income signal boost.
var incomeActionTypes = map[string]bool{
	"propose_income_action": true,
	"analyze_opportunity":   true,
	"schedule_work":         true,
}

// IsIncomeAction reports whether the given action type is income-oriented.
func IsIncomeAction(actionType string) bool {
	return incomeActionTypes[actionType]
}

// MapOpportunityToActions returns the agent action type strings associated with
// the given opportunity type. The returned slice is always non-empty.
func MapOpportunityToActions(opportunityType string) []string {
	switch opportunityType {
	case "consulting":
		return []string{"propose_income_action", "schedule_work"}
	case "automation":
		return []string{"propose_income_action", "analyze_opportunity"}
	case "service":
		return []string{"propose_income_action", "analyze_opportunity", "schedule_work"}
	case "content":
		return []string{"propose_income_action"}
	default: // "other" and unknown types
		return []string{"propose_income_action"}
	}
}
