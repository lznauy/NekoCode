// Package modelretry applies the retry policy for model requests.
package modelretry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"nekocode/util/http"
)

// Config defines the exponential backoff parameters.
type Config struct {
	MaxAttempts int // total attempts including the first one (not retry count)
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// When the legacy provider package is linked its exported variable remains
// the source of defaults. Standalone harness users need no provider factory.
var legacyDefault atomic.Pointer[Config]

func Default() Config {
	if cfg := legacyDefault.Load(); cfg != nil {
		return *cfg
	}
	return Config{MaxAttempts: 4, BaseDelay: 500 * time.Millisecond, MaxDelay: 8 * time.Second}
}

// BindLegacyDefault is called once by the provider package at initialization.
func BindLegacyDefault(cfg *Config) { legacyDefault.Store(cfg) }

func isRetryableStatus(code int) bool {
	switch code {
	case 408: // Request Timeout
		return true
	case 429: // Rate Limit / Too Many Requests
		return true
	default:
		return code >= 500 // all server errors
	}
}

// IsRetryable classifies LLM errors into retryable vs terminal.
//
// Priority order:
//  1. Context cancellation / deadline exceeded → terminal (caller gave up).
//  2. HTTP status code extracted from typed *http.HTTPError → classified by range:
//     408, 429, 5xx = retryable; 4xx = terminal.
//  3. Keyword fallback for non-HTTP errors (network-level, DNS, etc.).
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var httpErr *http.HTTPError
	if errors.As(err, &httpErr) {
		return isRetryableStatus(httpErr.StatusCode)
	}

	// Network-level errors — don't carry HTTP codes.
	msg := err.Error()
	for _, kw := range retryableKeywords {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

var retryableKeywords = []string{
	"connection refused",
	"connection reset",
	"no such host",
	"i/o timeout",
	"TLS handshake timeout",
	"EOF",
}

// Retry executes fn with exponential backoff.
func Retry(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error
	for i := 0; i < cfg.MaxAttempts; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !IsRetryable(err) {
			return err
		}
		if i == cfg.MaxAttempts-1 {
			break
		}
		delay := min(cfg.BaseDelay*time.Duration(1<<i), cfg.MaxDelay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("max attempts (%d) exceeded: %w", cfg.MaxAttempts, lastErr)
}
