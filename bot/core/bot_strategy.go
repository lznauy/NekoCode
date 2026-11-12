package core

import (
	"context"
	"fmt"

	"nekocode/bot/agent"
	"nekocode/bot/provider/types"
	"nekocode/mode"
)

type interactionRun func(*agent.Agent, context.Context, string, []types.MessageImage, agent.RunCallback) *agent.RunResult

// Bind execution in the host, not the mode definition package. Both current
// strategies reuse the agent loop; the captured tool policy governs dispatch.
func interactionStrategy(strategy mode.Strategy) (interactionRun, error) {
	switch strategy {
	case mode.Direct, mode.Delegated:
		return (*agent.Agent).RunContext, nil
	default:
		return nil, fmt.Errorf("execution strategy %q is unavailable", strategy)
	}
}
