package protocol

import "context"

// AccessObservation is a sampled view of declared rules and current authority,
// not an approval, transaction, or complete fingerprint of callback behavior.
type AccessObservation struct {
	Schema               int    `json:"schema"`
	Mode                 string `json:"mode"`
	PlanMode             bool   `json:"plan_mode"`
	RulesHash            string `json:"rules_hash,omitempty"`
	GrantsHash           string `json:"grants_hash,omitempty"`
	WorkspaceHash        string `json:"workspace_hash,omitempty"`
	StoreUnavailable     bool   `json:"store_unavailable,omitempty"`
	ShellJudgeConfigured bool   `json:"shell_judge_configured,omitempty"`
	WebJudgeConfigured   bool   `json:"web_judge_configured,omitempty"`
}

type accessObservationKey struct{}

func WithAccessObservation(ctx context.Context, observation AccessObservation) context.Context {
	return context.WithValue(ctx, accessObservationKey{}, observation)
}

func AccessObservationFromContext(ctx context.Context) AccessObservation {
	if ctx == nil {
		return AccessObservation{}
	}
	observation, _ := ctx.Value(accessObservationKey{}).(AccessObservation)
	return observation
}
