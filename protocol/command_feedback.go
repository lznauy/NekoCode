package protocol

import "context"

type commandStatusDisplayKey struct{}

// WithCommandStatusDisplay marks a client that displays setting changes in its status UI.
func WithCommandStatusDisplay(ctx context.Context) context.Context {
	return context.WithValue(ctx, commandStatusDisplayKey{}, true)
}

func HasCommandStatusDisplay(ctx context.Context) bool {
	displayed, _ := ctx.Value(commandStatusDisplayKey{}).(bool)
	return displayed
}
