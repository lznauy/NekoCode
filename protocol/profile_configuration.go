package protocol

import "context"

type profileConfigurationKey struct{}

// WithProfileConfiguration binds an owner-generated, versioned declaration
// digest. It is diagnostic only, never a profile or authorization lookup key.
func WithProfileConfiguration(ctx context.Context, hash string) context.Context {
	return context.WithValue(ctx, profileConfigurationKey{}, hash)
}

func ProfileConfigurationFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	hash, _ := ctx.Value(profileConfigurationKey{}).(string)
	return hash
}
