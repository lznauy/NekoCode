package toolpolicy

import (
	"context"
	"time"

	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/policy"
)

// ObservePermissions attaches audit-only hooks to the actor-local executor.
// The parent governance handle observes subagents without granting them any of
// its ledger evidence, and callback results never become approval decisions.
func ObservePermissions(executor *runner.Executor, gov *policy.Policy, actor string) {
	if gov == nil {
		return
	}
	executor.SetPermissionObserver(func(ctx context.Context, event runner.PermissionEvent) {
		point := policy.PermissionRequest
		if event.Denied {
			point = policy.PermissionDenied
			// A dismissed approval may also cancel the run. Still give its observer
			// bounded time to record the explicit denial.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			ctx = cleanup
		}
		gov.Observe(ctx, point, policy.Facts{ActorID: actor, Tool: policy.ToolFacts{Name: event.Tool, Args: event.Args, Error: event.Denied, ErrorMessage: event.Reason}})
	})
}
