package policy

import (
	"fmt"
	"math"
)

// FilterAndApply validates proposals against safety bounds, limits the count
// per cycle, and returns only the proposals that are safe to apply.
// It is pure and deterministic.
func FilterAndApply(proposals []PolicyChange, stabilityMode string) (safe []PolicyChange, rejected []PolicyChange) {
	for i := range proposals {
		p := &proposals[i]

		// Reject if stability is in safe_mode — no parameter changes allowed.
		if stabilityMode == "safe_mode" {
			rejected = append(rejected, *p)
			continue
		}

		// Reject if confidence is too low.
		if p.Confidence < MinConfidence {
			rejected = append(rejected, *p)
			continue
		}

		// Clamp delta to MaxDelta.
		bounds := ParamBounds(p.Parameter)
		if math.Abs(p.Delta) > bounds.MaxDelta {
			if p.Delta > 0 {
				p.Delta = bounds.MaxDelta
			} else {
				p.Delta = -bounds.MaxDelta
			}
			p.NewValue = p.OldValue + p.Delta
		}

		// Clamp new value to bounds.
		p.NewValue = clamp(p.NewValue, bounds.Min, bounds.Max)
		p.Delta = p.NewValue - p.OldValue

		// Skip no-op changes.
		if math.Abs(p.Delta) < 1e-9 {
			continue
		}

		safe = append(safe, *p)
		if len(safe) >= MaxChangesPerCycle {
			// Remaining proposals are rejected due to cycle limit.
			for j := i + 1; j < len(proposals); j++ {
				rejected = append(rejected, proposals[j])
			}
			break
		}
	}
	return safe, rejected
}

// ValidateChange checks a single change against safety constraints.
// Returns an error if the change is invalid.
func ValidateChange(c PolicyChange) error {
	bounds := ParamBounds(c.Parameter)

	if math.Abs(c.Delta) > bounds.MaxDelta+1e-9 {
		return fmt.Errorf("delta %.4f exceeds max %.4f for %s", c.Delta, bounds.MaxDelta, c.Parameter)
	}
	if c.NewValue < bounds.Min-1e-9 || c.NewValue > bounds.Max+1e-9 {
		return fmt.Errorf("new_value %.4f outside bounds [%.2f, %.2f] for %s", c.NewValue, bounds.Min, bounds.Max, c.Parameter)
	}
	return nil
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
