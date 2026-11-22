package core

import (
	"fmt"
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool/runtime/taskbridge"
)

// Resolve the attempt limit after loading the actual profile, including on resume.
// Explicit requests must fit the profile; defaults may be lowered by the host.
func resolveTaskStepBudget(profile agentprofile.Profile, spec taskbridge.TaskSpec, defaultSteps, batchCeiling int) (agentprofile.Profile, error) {
	limit := profile.MaxSteps
	if limit == 0 {
		limit = agentprofile.DefaultMaxSteps
	}
	if limit < 1 || limit > agentprofile.MaxSteps {
		return profile, fmt.Errorf("invalid profile step limit %d", limit)
	}
	if spec.MaxSteps < 0 || spec.MaxSteps > min(limit, taskbridge.MaxRequestedSteps) {
		return profile, fmt.Errorf("max_steps must be between 1 and %d when supplied", min(limit, taskbridge.MaxRequestedSteps))
	}
	if spec.MaxSteps > 0 {
		limit = spec.MaxSteps
	} else if defaultSteps > 0 {
		limit = min(defaultSteps, limit)
	}
	if batchCeiling > 0 {
		limit = min(limit, batchCeiling)
	}
	profile = agentprofile.Clone(profile)
	profile.MaxSteps = limit
	return profile, nil
}
