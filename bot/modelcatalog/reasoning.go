package modelcatalog

import "encoding/json"

// Reasoning describes capability only. Wire parameters and replay policies
// belong to local protocol adapters. Nil metadata means unknown, not false.
type Reasoning struct {
	Supported bool     `json:"supported"`
	Efforts   []string `json:"efforts,omitempty"`
	Toggle    bool     `json:"toggle,omitempty"`
	Budget    bool     `json:"budget,omitempty"`
}

func cloneLimits(l Limits) Limits {
	if l.Reasoning != nil {
		r := *l.Reasoning
		r.Efforts = append([]string(nil), r.Efforts...)
		l.Reasoning = &r
	}
	return l
}

func validEffort(value string) bool {
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}
func validReasoning(r *Reasoning) bool {
	if r == nil {
		return true
	}
	if !r.Supported && (len(r.Efforts) > 0 || r.Toggle || r.Budget) {
		return false
	}
	seen := map[string]bool{}
	for _, effort := range r.Efforts {
		if !validEffort(effort) || seen[effort] {
			return false
		}
		seen[effort] = true
	}
	return true
}
func parseReasoning(supported *bool, raw json.RawMessage) *Reasoning {
	if supported == nil {
		return nil
	}
	if !*supported {
		if len(raw) > 0 && string(raw) != "null" && string(raw) != "[]" {
			return nil
		}
		return &Reasoning{}
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var options []struct {
		Type   string            `json:"type"`
		Values []json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(raw, &options); err != nil || len(options) == 0 {
		return nil
	}
	r := &Reasoning{Supported: true}
	seen := map[string]bool{}
	for _, option := range options {
		switch option.Type {
		case "effort":
			if len(option.Values) == 0 {
				return nil
			}
			for _, value := range option.Values {
				// null/default mean provider default, already represented by auto.
				if string(value) == "null" {
					continue
				}
				var effort string
				if err := json.Unmarshal(value, &effort); err != nil {
					return nil
				}
				if effort == "default" || effort == "null" {
					continue
				}
				if !validEffort(effort) {
					return nil
				}
				if !seen[effort] {
					r.Efforts = append(r.Efforts, effort)
					seen[effort] = true
				}
			}
		case "toggle":
			r.Toggle = true
		case "budget_tokens":
			r.Budget = true
		default:
			return nil
		}
	}
	return r
}

// Origin is an author-owned model capability reference. It deliberately carries
// no context/output limits, endpoint, credentials, or routing instructions.
type Origin struct {
	Provider  string     `json:"provider"`
	ModelID   string     `json:"model"`
	Reasoning *Reasoning `json:"reasoning"`
}

func (c *Catalog) OriginReasoning(model string) (Origin, bool) {
	if c == nil {
		c = Bundled()
	}
	origin, ok := c.data.Origins[model]
	if ok {
		origin.Reasoning = cloneLimits(Limits{Reasoning: origin.Reasoning}).Reasoning
	}
	return origin, ok
}
