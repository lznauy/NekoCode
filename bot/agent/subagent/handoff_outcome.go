package subagent

import (
	"fmt"
	"nekocode/bot/provider/types"
	"strings"
)

func handoffOutcomeSchema() types.Property {
	return types.Property{Type: "string", Enum: []string{"complete", "continue", "blocked", "decision"}, Description: "Scheduling, not quality. complete requires unfinished=[]. continue=more scoped work; blocked=missing prerequisite; decision=coordinator choice. Requires unfinished and next_action. Blocked/decision bypass batching."}
}
func optionalHandoffOutcome(args map[string]any) (string, string, error) {
	outcome := ""
	next := ""
	if raw, ok := args["outcome"]; ok {
		var valid bool
		outcome, valid = raw.(string)
		if !valid {
			return "", "", fmt.Errorf("outcome must be a string")
		}
		if outcome == "" {
			return "", "", fmt.Errorf("explicit outcome cannot be empty")
		}
	}
	if raw, ok := args["next_action"]; ok {
		var valid bool
		next, valid = raw.(string)
		if !valid || len(next) > 1024 {
			return "", "", fmt.Errorf("next_action must be a string of at most 1024 bytes")
		}
		next = strings.TrimSpace(next)
	}
	return outcome, next, nil
}
