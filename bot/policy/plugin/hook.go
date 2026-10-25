package plugin

import (
	"fmt"
	"nekocode/bot/policy/contract"
	"strings"
)

func makePluginHook(point Point, eventType, pluginRoot string, eh eventHook, requireError bool) Hook {
	matcher, matcherErr := compileMatcher(eh.Matcher)
	return Hook{
		Name:  fmt.Sprintf("plugin:%s:%s", eventType, eh.Matcher),
		Point: point,
		On: func(e Event) *Result {
			if matcherErr != nil {
				return &Result{Err: matcherErr}
			}
			if !matcher.MatchString(e.Tool) {
				return nil
			}
			if requireError && !e.Error {
				return nil
			}
			result := runPluginActions(pluginRoot, eventType, e, eh.Hooks)
			if result != nil && eh.Mode == "observer" {
				d, err := contract.Control(*result)
				if err != nil || d.Action != contract.Continue {
					return &Result{Err: fmt.Errorf("observer hook cannot control execution")}
				}
			}
			return result
		},
	}
}

// Every action runs in declaration order. State patches are staged until the
// complete hook result has passed engine validation.
func runPluginActions(pluginRoot, eventType string, event Event, actions []hookAction) *Result {
	event = contract.NormalizeEvent(event)
	event.PluginRoot = pluginRoot
	if event.Point == "" {
		for _, spec := range eventSpecs {
			if spec.Name == eventType {
				event.Point = string(spec.Point)
				break
			}
		}
	}
	var results []Result
	for index, action := range actions {
		event.ActionIndex = index
		var result *Result
		var err error
		switch action.Type {
		case "command":
			var output string
			var truncated bool
			output, truncated, err = runPluginCommandEvent(pluginRoot, event, action)
			if err == nil {
				trimmed := strings.TrimSpace(output)
				if truncated && action.OutputFormat == "json" {
					err = fmt.Errorf("command output exceeds %d bytes", maxPluginOutputBytes)
				} else if trimmed != "" {
					if err = validatePluginOutput(action.OutputSchema, trimmed); err == nil {
						if action.OutputFormat == "json" {
							result, err = decodeStructuredResult([]byte(trimmed))
						} else {
							result = &Result{Hint: &Hint{Type: "plugin_output", Severity: "info", Content: formatPluginOutput(eventType, action.Command, trimmed, truncated)}}
						}
					}
				}
			}
		case "js":
			result, err = runPluginJS(pluginRoot, event, action)
		default:
			err = fmt.Errorf("unsupported action type %q", action.Type)
		}
		if err != nil {
			return &Result{Err: fmt.Errorf("%s: %w", eventType, err)}
		}
		if result != nil {
			if err := contract.Validate(event.Point, result); err != nil {
				return &Result{Err: err}
			}
			normalized := contract.Normalize(event.Point, event.Tool, *result)
			results = append(results, normalized)
		}
	}
	switch len(results) {
	case 0:
		return nil
	case 1:
		// Each action was validated above; preserve its legacy projection.
		return &results[0]
	}
	merged, err := contract.Merge(results)
	if err != nil {
		return &Result{Err: err}
	}
	return &merged
}
