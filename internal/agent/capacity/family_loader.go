package capacity

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// familyContextFile mirrors the top-level structure of family_context.yaml.
type familyContextFile struct {
	Family struct {
		Constraints struct {
			MaxDailyWorkHours  float64 `yaml:"max_daily_work_hours"`
			MinFamilyTimeHours float64 `yaml:"minimum_family_time_hours"`
			StressLimit        string  `yaml:"stress_limit"`
		} `yaml:"constraints"`
	} `yaml:"family"`
	Time struct {
		WorkingHours struct {
			Preferred []string `yaml:"preferred"`
		} `yaml:"working_hours"`
		BlockedTime []struct {
			Reason string `yaml:"reason"`
			Range  string `yaml:"range"`
		} `yaml:"blocked_time"`
	} `yaml:"time"`
}

// LoadFamilyConfig reads the family_context.yaml file and returns a FamilyConfig.
// Fail-open: returns defaults if the file is missing or invalid.
func LoadFamilyConfig(path string) FamilyConfig {
	cfg := FamilyConfig{
		MaxDailyWorkHours:  DefaultMaxDailyWorkHours,
		MinFamilyTimeHours: DefaultMinFamilyTimeHours,
	}

	if path == "" {
		return cfg
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	var raw familyContextFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg
	}

	if raw.Family.Constraints.MaxDailyWorkHours > 0 {
		cfg.MaxDailyWorkHours = raw.Family.Constraints.MaxDailyWorkHours
	}
	if raw.Family.Constraints.MinFamilyTimeHours > 0 {
		cfg.MinFamilyTimeHours = raw.Family.Constraints.MinFamilyTimeHours
	}
	cfg.StressLimit = raw.Family.Constraints.StressLimit

	for _, bt := range raw.Time.BlockedTime {
		cfg.BlockedRanges = append(cfg.BlockedRanges, BlockedTime{
			Reason: bt.Reason,
			Range:  bt.Range,
		})
	}
	cfg.WorkingWindows = raw.Time.WorkingHours.Preferred

	return cfg
}

// ComputeBlockedHours computes the total blocked hours from blocked time ranges.
// Ranges are in "HH:MM-HH:MM" format.
func ComputeBlockedHours(ranges []BlockedTime) float64 {
	total := 0.0
	for _, bt := range ranges {
		d := parseRangeDuration(bt.Range)
		total += d
	}
	return total
}

// parseRangeDuration parses "HH:MM-HH:MM" and returns the duration in hours.
func parseRangeDuration(r string) float64 {
	parts := strings.SplitN(r, "-", 2)
	if len(parts) != 2 {
		return 0
	}
	start, err1 := parseHHMM(parts[0])
	end, err2 := parseHHMM(parts[1])
	if err1 != nil || err2 != nil {
		return 0
	}
	d := end.Sub(start).Hours()
	if d < 0 {
		d += 24 // crosses midnight
	}
	return d
}

// parseHHMM parses "HH:MM" into a time.Time on a zero date.
func parseHHMM(s string) (time.Time, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time: %s", s)
	}
	return t, nil
}
