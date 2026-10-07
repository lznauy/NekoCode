// Package agentrunner adapts the NekoCode agent core to the interaction
// runtime. Applications remain responsible only for constructing the agent
// with their prompt, model, context, policy, and tool registry.
package agentrunner

import (
	"context"

	agentcore "nekocode/bot/agent"
	controlruntime "nekocode/runtime"
)

// Runner exposes one agent as a runtime Runner with streaming, tool events,
// cancellation, metrics, and lifecycle handling.
type Runner struct {
	agent *agentcore.Agent
}

func New(agent *agentcore.Agent) *Runner {
	if agent == nil {
		panic("runtime/agentrunner: nil agent")
	}
	return &Runner{agent: agent}
}

func (r *Runner) Run(ctx context.Context, input string, host controlruntime.RunHost) (string, error) {
	releaseOutput := r.agent.BindOutput(agentcore.Output{
		Text:   host.Text,
		Reason: host.Reason,
		Phase:  host.Phase,
	})
	defer releaseOutput()
	releaseInteraction := r.agent.BindInteraction(agentcore.Interaction{
		Confirm: host.Confirm,
		Ask:     host.Ask,
		Todos:   host.Todos,
	})
	defer releaseInteraction()

	finished := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			r.agent.Abort()
		case <-finished:
		}
	}()
	defer func() {
		close(finished)
		<-watchDone
	}()

	result := r.agent.Run(input, nil, host.Step)
	return result.FinalOutput, result.Error
}

func (r *Runner) Metrics() controlruntime.MetricsSnapshot {
	return r.agent.Metrics()
}

func (r *Runner) Close() error {
	r.agent.Abort()
	return nil
}

// Services returns the optional runtime functions implemented by Runner.
func (r *Runner) Services() controlruntime.Services {
	return controlruntime.Services{Metrics: r.Metrics, Close: r.Close}
}

var _ controlruntime.Runner = (*Runner)(nil)
