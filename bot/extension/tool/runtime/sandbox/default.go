package sandbox

import (
	"context"
	"time"

	"nekocode/bot/extension/tool/runtime/sandbox/impl"
)

// Run executes a command using the best available OS backend.
func Run(ctx context.Context, command string, profile Profile, timeout time.Duration) (string, error) {
	return impl.Run(ctx, command, profile, timeout)
}

func Start(ctx context.Context, command string, profile Profile) (*Process, error) {
	return impl.Start(ctx, command, profile)
}

func RunHost(ctx context.Context, command string, timeout time.Duration) (string, error) {
	return impl.RunHost(ctx, command, timeout)
}

func StartHost(ctx context.Context, command string) (*Process, error) {
	return impl.StartHost(ctx, command)
}

func StartHostIn(ctx context.Context, command, directory string) (*Process, error) {
	return impl.StartHostIn(ctx, command, directory)
}

func IsAvailable() bool { return impl.IsAvailable() }
