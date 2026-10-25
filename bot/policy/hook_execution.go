package policy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nekocode/bot/policy/contract"
)

// executeHook runs outside engine locks. Only the caller may commit its private
// candidate state after checking the registration and session are still current.
func executeHook(ctx context.Context, h Hook, state *hookState) (*Result, bool) {
	hookCtx, cancel := context.WithTimeout(ctx, hookTimeout(h.Timeout))
	result, err := invokeHook(hookCtx, h, state)
	if err == nil {
		err = contract.Validate(string(h.Point), result)
	}
	if err == nil && hookCtx.Err() != nil {
		err = hookCtx.Err()
	}
	cancel()
	if err == nil && result != nil {
		normalized := contract.Normalize(string(h.Point), state.facts.Tool.Name, *result)
		result = &normalized
		if result.StatePatch != nil {
			applyStatePatch(state, result.StatePatch)
		}
	}
	if err == nil {
		err = contract.ValidateState(contract.StatePatch{Ints: state.ints, Strings: state.strings})
	}
	valid := err == nil
	if err != nil {
		result = hookFailure(h, err)
	}
	return result, valid
}

func hookTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return 5 * time.Second
	}
	return d
}

// In-process Go callbacks are trusted and must cooperate with cancellation.
// Running them in abandoned goroutines would leak work and mutable state.
func invokeHook(ctx context.Context, h Hook, s State) (r *Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("callback panic: %v", p)
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if h.Run != nil {
		return h.Run(ctx, s)
	}
	if h.On == nil {
		return nil, errors.New("missing hook handler")
	}
	return h.On(s), nil
}

func evaluationCanceled(parent, ctx context.Context) Result {
	reason := StopPolicyBlocked
	if parent.Err() != nil {
		reason = StopInterrupted
	}
	return Result{Decision: &Decision{Action: Terminate, StopReason: reason, Reason: ctx.Err().Error()}}
}
