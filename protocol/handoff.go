package protocol

import (
	"fmt"
	"strings"
)

// HandoffIncomplete is the single completion rule for workers and public hosts.
// Scheduling metadata never certifies output quality or grants permissions.
func HandoffIncomplete(outcome string, unfinished []string) bool {
	// The actionable work list is the sole completion signal. Outcome remains
	// accepted for older callers, but cannot contradict this list.
	return len(unfinished) > 0
}

// ValidateHandoff is shared by every handoff entrypoint, before previews are
// bounded. Empty outcome is the legacy protocol, not an assertion of completion.
func ValidateHandoff(outcome, next string, unfinished []string) error {
	if len(next) > 1024 {
		return fmt.Errorf("next_action exceeds 1024 bytes")
	}
	switch outcome {
	case "": // Legacy reports use their unfinished list.
	case "complete":
		if len(unfinished) != 0 {
			return fmt.Errorf("complete handoff cannot contain unfinished work")
		}
	case "continue", "blocked", "decision":
		if strings.TrimSpace(next) == "" || len(unfinished) == 0 {
			return fmt.Errorf("non-complete handoff requires next_action and unfinished work")
		}
	default:
		return fmt.Errorf("outcome must be complete, continue, blocked or decision")
	}
	return nil
}
