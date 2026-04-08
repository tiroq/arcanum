package arbitration

// ArbitrationTrace captures the full decision-making record for a single path arbitration.
type ArbitrationTrace struct {
	PathSignature     string             `json:"path_signature"`
	RawSignals        []Signal           `json:"raw_signals"`
	SuppressedSignals []SuppressedSignal `json:"suppressed_signals"`
	AppliedSignals    []AppliedSignal    `json:"applied_signals"`
	WinningSignal     *Signal            `json:"winning_signal,omitempty"`
	RulesApplied      []string           `json:"rules_applied"`
	FinalAdjustment   float64            `json:"final_adjustment"`
	Reason            string             `json:"reason"`
}

// SuppressedSignal records a signal that was excluded from the final computation.
type SuppressedSignal struct {
	Signal Signal `json:"signal"`
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

// AppliedSignal records a signal that contributed to the final adjustment.
type AppliedSignal struct {
	Signal     Signal  `json:"signal"`
	Adjustment float64 `json:"adjustment"`
}

// TraceSnapshot holds a batch of arbitration traces for API visibility.
type TraceSnapshot struct {
	Traces []ArbitrationTrace `json:"traces"`
}
