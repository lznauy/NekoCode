package protocol

import (
	"context"
	"strings"
	"unicode"
)

type commandStatusDisplayKey struct{}

// WithCommandStatusDisplay marks a client that displays setting changes in its status UI.
func WithCommandStatusDisplay(ctx context.Context) context.Context {
	return context.WithValue(ctx, commandStatusDisplayKey{}, true)
}

func HasCommandStatusDisplay(ctx context.Context) bool {
	displayed, _ := ctx.Value(commandStatusDisplayKey{}).(bool)
	return displayed
}

// IsSlashCommandInput distinguishes a command token from a multi-component
// absolute path. Keep single-component names for commands and typo feedback.
func IsSlashCommandInput(input string) bool {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return false
	}
	if i := strings.IndexFunc(input, unicode.IsSpace); i >= 0 {
		input = input[:i]
	}
	return !strings.Contains(input[1:], "/")
}
