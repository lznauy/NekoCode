package plugin

import (
	"fmt"
	"strings"
)

func makePluginHook(point Point, eventType, pluginRoot string, eh eventHook, requireError bool) Hook {
	return Hook{
		Name:  fmt.Sprintf("plugin:%s:%s", eventType, eh.Matcher),
		Point: point,
		On: func(e Event) *Result {
			if !matchTool(eh.Matcher, e.Tool) {
				return nil
			}
			if requireError && !e.Error {
				return nil
			}
			return runFirstPluginAction(pluginRoot, eventType, eh.Hooks)
		},
	}
}

func runFirstPluginAction(pluginRoot, eventType string, actions []hookAction) *Result {
	for _, action := range actions {
		if action.Type != "command" {
			continue
		}
		output, truncated, runErr := runPluginCommand(pluginRoot, action)
		if runErr != nil {
			return pluginErrorResult(eventType, action.Command, runErr)
		}

		trimmed := strings.TrimSpace(output)
		if trimmed == "" {
			continue
		}
		if schemaErr := validatePluginOutput(action.OutputSchema, trimmed); schemaErr != nil {
			return &Result{Hint: &Hint{Type: "plugin_schema_error", Severity: "info",
				Content: formatPluginOutput(eventType, action.Command, schemaErr.Error(), false)}}
		}
		return &Result{Hint: &Hint{Type: "plugin_output", Severity: "info",
			Content: formatPluginOutput(eventType, action.Command, trimmed, truncated)}}
	}
	return nil
}

func pluginErrorResult(eventType, command string, err error) *Result {
	errMsg := err.Error()
	if len(errMsg) > maxPluginOutputBytes {
		errMsg = errMsg[:maxPluginOutputBytes]
	}
	return &Result{Hint: &Hint{Type: "plugin_error", Severity: "info",
		Content: formatPluginOutput(eventType, command, fmt.Sprintf("Hook failed: %s", errMsg), false)}}
}

func validatePluginOutput(schema []byte, output string) error {
	if schema == nil {
		return nil
	}
	if !isValidJSON(output) {
		return fmt.Errorf("Output is not valid JSON (output_schema specified). Output rejected.")
	}
	if err := validateAgainstSchema(schema, output); err != nil {
		return fmt.Errorf("Schema validation failed: %v. Output rejected.", err)
	}
	return nil
}
