package llm

import (
	"context"
	"fmt"
	"time"

	"nekocode/bot/provider/types"
)

const streamIdleTimeout = 3 * time.Minute

var ErrStreamIdle = fmt.Errorf("stream idle timeout: no tokens for %v", streamIdleTimeout)

// ReadStream consumes one physical response. A terminal error can arrive
// before token closure. Deliver all tokens (including partial text and usage)
// and preserve the first incomplete finish signal across later metadata.
func ReadStream(ctx context.Context, tokens <-chan types.StreamToken, errs <-chan error, consume func(types.StreamToken) error) error {
	timer := time.NewTimer(streamIdleTimeout)
	defer timer.Stop()
	var providerErr, completionErr error
	for tokens != nil || errs != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case providerErr = <-errs:
			errs = nil
		case token, ok := <-tokens:
			if !ok {
				tokens = nil
				continue
			}
			timer.Reset(streamIdleTimeout)
			if completionErr == nil {
				completionErr = types.StreamCompletionError(token.FinishReason, token.Refused)
			}
			if err := consume(token); err != nil {
				return err
			}
		case <-timer.C:
			if providerErr != nil {
				return providerErr
			}
			return ErrStreamIdle
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if providerErr != nil {
		return providerErr
	}
	return completionErr
}
