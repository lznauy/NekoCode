package llm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// RetryConfig defines the exponential backoff parameters.
type RetryConfig struct {
	MaxAttempts int // total attempts including the first one (not retry count)
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 4,
	BaseDelay:   500 * time.Millisecond,
	MaxDelay:    8 * time.Second,
}

// ---- HTTP status extraction -------------------------------------------------

// apiErrorRE matches error formats used by Chat/ChatStream:
//
//	"API error (HTTP 429): ..."   (OpenAI-compat)
var apiErrorRE = regexp.MustCompile(`API error(?: \(HTTP (\d+)\))?: (\d+)`)

func extractHTTPStatus(err error) (int, bool) {
	msg := err.Error()
	m := apiErrorRE.FindStringSubmatch(msg)
	if m == nil {
		return 0, false
	}
	// Group 1: code from "(HTTP %d)" format; Group 2: code from "status string" format.
	for _, g := range []string{m[1], m[2]} {
		if g != "" {
			var code int
			fmt.Sscanf(g, "%d", &code)
			if code >= 100 && code < 600 {
				return code, true
			}
		}
	}
	return 0, false
}

// ---- Retry classification --------------------------------------------------

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
//  2. HTTP status code extracted from the error → classified by range:
//     408, 429, 5xx = retryable; 4xx = terminal.
//  3. Keyword fallback for non-HTTP errors (network-level, DNS, etc.).
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if code, ok := extractHTTPStatus(err); ok {
		return isRetryableStatus(code)
	}

	// Network-level errors — don't carry HTTP codes.
	msg := err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
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
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
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
