package profile

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseProfile parses a profile DSL string into an ordered slice of ModelCandidate.
//
// DSL format: "model?key=value&key=value|model2?key=value"
//
// Candidates are separated by "|". Parameters start after "?" and are separated by "&".
//
// Supported keys:
//   - think: thinking mode (thinking, nothinking, on, off, default)
//   - timeout: per-candidate timeout in seconds
//   - json: whether to request JSON output (true, false)
//
// Examples:
//
//	"qwen2.5:7b-instruct?think=on&timeout=240&json=true"
//	"qwen2.5:7b-instruct?think=on&timeout=240|llama3.2:3b?think=off&timeout=60"
func ParseProfile(dsl string) ([]ModelCandidate, error) {
	dsl = strings.TrimSpace(dsl)
	if dsl == "" {
		return nil, fmt.Errorf("empty profile DSL")
	}

	segments := strings.Split(dsl, "|")
	candidates := make([]ModelCandidate, 0, len(segments))

	for i, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}

		c, err := parseCandidate(seg)
		if err != nil {
			return nil, fmt.Errorf("candidate %d: %w", i, err)
		}

		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("candidate %d: %w", i, err)
		}

		candidates = append(candidates, c)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("profile DSL produced no candidates")
	}

	return candidates, nil
}

func parseCandidate(seg string) (ModelCandidate, error) {
	// Split on "?" to separate model name from parameters.
	qIdx := strings.Index(seg, "?")
	if qIdx < 0 {
		// No parameters — just a model name.
		name := strings.TrimSpace(seg)
		if name == "" {
			return ModelCandidate{}, fmt.Errorf("missing model name")
		}
		return ModelCandidate{ModelName: name}, nil
	}

	name := strings.TrimSpace(seg[:qIdx])
	if name == "" {
		return ModelCandidate{}, fmt.Errorf("missing model name")
	}

	c := ModelCandidate{
		ModelName: name,
	}

	paramStr := seg[qIdx+1:]
	if paramStr == "" {
		return c, nil
	}

	params := strings.Split(paramStr, "&")
	for _, kv := range params {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}

		eqIdx := strings.Index(kv, "=")
		if eqIdx < 0 {
			return ModelCandidate{}, fmt.Errorf("invalid key-value pair %q: missing '='", kv)
		}

		key := strings.TrimSpace(kv[:eqIdx])
		val := strings.TrimSpace(kv[eqIdx+1:])

		switch strings.ToLower(key) {
		case "think":
			tm, err := ParseThinkMode(val)
			if err != nil {
				return ModelCandidate{}, err
			}
			c.ThinkMode = tm
		case "timeout":
			secs, err := strconv.Atoi(val)
			if err != nil {
				return ModelCandidate{}, fmt.Errorf("invalid timeout %q: %w", val, err)
			}
			c.Timeout = time.Duration(secs) * time.Second
		case "json":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return ModelCandidate{}, fmt.Errorf("invalid json value %q: %w", val, err)
			}
			c.JSONMode = b
		case "provider":
			if val == "" {
				return ModelCandidate{}, fmt.Errorf("provider name must not be empty")
			}
			c.ProviderName = val
		default:
			return ModelCandidate{}, fmt.Errorf("unknown option %q", key)
		}
	}

	return c, nil
}

// ParseProfileOrSingle parses a profile DSL string, or if the string looks like a
// plain model name (no "?" or "|"), wraps it as a single-candidate profile.
// This provides backward compatibility with existing OLLAMA_FAST_MODEL-style env vars.
func ParseProfileOrSingle(s string) ([]ModelCandidate, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty profile string")
	}

	if !strings.Contains(s, "?") && !strings.Contains(s, "|") {
		return []ModelCandidate{{ModelName: s}}, nil
	}

	return ParseProfile(s)
}
