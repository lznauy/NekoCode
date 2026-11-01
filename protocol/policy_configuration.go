package protocol

import "context"

// PolicyConfiguration is observation only. Revision is process-local; Hash is
// stable across restarts for the same ordered declarations. Neither grants access.
type PolicyConfiguration struct {
	Schema     int    `json:"schema"`
	Hash       string `json:"hash"`
	Revision   uint64 `json:"revision"`
	Incomplete bool   `json:"incomplete"`
}

type policyConfigurationKey struct{}

func WithPolicyConfiguration(ctx context.Context, configuration PolicyConfiguration) context.Context {
	return context.WithValue(ctx, policyConfigurationKey{}, configuration)
}

func PolicyConfigurationFromContext(ctx context.Context) PolicyConfiguration {
	if ctx == nil {
		return PolicyConfiguration{}
	}
	configuration, _ := ctx.Value(policyConfigurationKey{}).(PolicyConfiguration)
	return configuration
}
