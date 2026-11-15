package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const pollErrorDelay = 3 * time.Second

type retryAfterError struct {
	cause error
	delay time.Duration
}

func (e *retryAfterError) Error() string { return e.cause.Error() }
func (e *retryAfterError) Unwrap() error { return e.cause }

func withRetryAfter(cause error, body []byte, header string, now time.Time) error {
	var response struct {
		Parameters struct {
			RetryAfter json.Number `json:"retry_after"`
		} `json:"parameters"`
	}
	var delay time.Duration
	if json.Unmarshal(body, &response) == nil {
		if seconds, err := response.Parameters.RetryAfter.Int64(); err == nil {
			delay = retrySeconds(seconds)
		}
	}
	header = strings.TrimSpace(header)
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil {
		delay = max(delay, retrySeconds(seconds))
	} else if until, err := http.ParseTime(header); err == nil {
		delay = max(delay, until.Sub(now))
	}
	if delay > 0 {
		return &retryAfterError{cause: cause, delay: delay}
	}
	return cause
}

func retrySeconds(seconds int64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if seconds > int64(maxDuration/time.Second) {
		return maxDuration
	}
	return time.Duration(seconds) * time.Second
}

func pollRetryDelay(err error) time.Duration {
	var limited *retryAfterError
	if errors.As(err, &limited) && limited.delay > 0 {
		return limited.delay
	}
	return pollErrorDelay
}

func waitPollRetry(ctx context.Context, delay time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
