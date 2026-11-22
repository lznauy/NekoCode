package protocol

import (
	"context"
	"strings"
)

// PreservedFile records the first observation of a user-designated original.
// An empty SHA256 means the original could not be verified at admission.
type PreservedFile struct {
	Root   string `json:"root"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
}

type goalTestKey struct{}
type goalTestCheck struct{ command, name string }

// WithGoalTest requests named test evidence for one exact acceptance command.
func WithGoalTest(ctx context.Context, command, name string) context.Context {
	return context.WithValue(ctx, goalTestKey{}, goalTestCheck{command, name})
}

func GoalTestForCommand(ctx context.Context, command string) string {
	check, _ := ctx.Value(goalTestKey{}).(goalTestCheck)
	if check.command == strings.TrimSpace(command) {
		return check.name
	}
	return ""
}

type goalSchedulingKey struct{}

// WithGoalScheduling tells the bot its interactive host can admit a new run
// after this one closes. Request/response hosts keep one run for the caller.
func WithGoalScheduling(ctx context.Context) context.Context {
	return context.WithValue(ctx, goalSchedulingKey{}, true)
}

func GoalScheduling(ctx context.Context) bool {
	enabled, _ := ctx.Value(goalSchedulingKey{}).(bool)
	return enabled
}

type goalContinuationKey struct{}

func WithGoalContinuation(ctx context.Context) context.Context {
	return context.WithValue(ctx, goalContinuationKey{}, true)
}

func IsGoalContinuation(ctx context.Context) bool {
	continuation, _ := ctx.Value(goalContinuationKey{}).(bool)
	return continuation
}

// IsGoalPauseCommand lets idle and active runs agree on the one goal control
// that can interrupt a running model call.
func IsGoalPauseCommand(input string) bool {
	if !strings.HasPrefix(strings.TrimSpace(input), "/") {
		return false
	}
	fields := strings.Fields(input)
	return len(fields) == 2 && strings.EqualFold(fields[0], "/goal") && strings.EqualFold(fields[1], "pause")
}
