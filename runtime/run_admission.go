package runtime

import "context"

type detachedRunKey struct{}

// WithDetachedRun keeps StartRun admission bound to ctx, but gives an accepted
// run its own cancellation lifetime. The runtime still cancels it on abort or
// shutdown. Ordinary StartRun callers retain their existing cancellation semantics.
func WithDetachedRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, detachedRunKey{}, true)
}
