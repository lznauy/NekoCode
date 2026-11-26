package provider

import (
	"context"

	"nekocode/bot/internal/modelretry"
)

// RetryConfig defines the exponential backoff parameters.
type RetryConfig modelretry.Config

// DefaultRetryConfig configures model retries. Set it before starting runs.
var DefaultRetryConfig = RetryConfig(modelretry.Default())

func init() { modelretry.BindLegacyDefault((*modelretry.Config)(&DefaultRetryConfig)) }

func IsRetryable(err error) bool { return modelretry.IsRetryable(err) }

// Retry executes fn with exponential backoff for retryable errors.
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
	return modelretry.Retry(ctx, modelretry.Config(cfg), fn)
}
