package vector

// GraphAdapter provides a nil-safe, fail-open adapter for the system vector.
type GraphAdapter struct {
	engine *Engine
}

// NewGraphAdapter creates a new adapter around the engine.
func NewGraphAdapter(engine *Engine) *GraphAdapter {
	return &GraphAdapter{engine: engine}
}

// GetVector returns the current system vector. Nil-safe, returns defaults.
func (a *GraphAdapter) GetVector() SystemVector {
	if a == nil || a.engine == nil {
		return DefaultVector()
	}
	return a.engine.GetVector()
}

// GetIncomePriority returns the income priority [0,1].
func (a *GraphAdapter) GetIncomePriority() float64 {
	return a.GetVector().IncomePriority
}

// GetFamilySafetyPriority returns the family safety priority [0,1].
func (a *GraphAdapter) GetFamilySafetyPriority() float64 {
	return a.GetVector().FamilySafetyPriority
}

// GetRiskTolerance returns the risk tolerance [0,1].
func (a *GraphAdapter) GetRiskTolerance() float64 {
	return a.GetVector().RiskTolerance
}

// GetHumanReviewStrictness returns the human review strictness [0,1].
func (a *GraphAdapter) GetHumanReviewStrictness() float64 {
	return a.GetVector().HumanReviewStrictness
}

// GetExplorationLevel returns the exploration level [0,1].
func (a *GraphAdapter) GetExplorationLevel() float64 {
	return a.GetVector().ExplorationLevel
}

// GetAutomationPriority returns the automation priority [0,1].
func (a *GraphAdapter) GetAutomationPriority() float64 {
	return a.GetVector().AutomationPriority
}
