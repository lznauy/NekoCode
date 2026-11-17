package config

import (
	"slices"
	"strings"

	"nekocode/bot/modelcatalog"
	"nekocode/bot/reasoning"
)

const ReasoningAuto = ""

// ReasoningCapability is the model-specific reasoning contract exposed to
// configuration and interactive clients. Efforts contains provider-native
// values only; Auto is always available and is represented by an empty value.
type ReasoningCapability struct {
	Source        string
	Reference     string
	Efforts       []string
	DisableEffort string
	ThinkingMode  string
	Replay        reasoning.ReplayPolicy
}

func (c ReasoningCapability) Supports(effort string) bool {
	effort, ok := ParseReasoningEffort(effort)
	if !ok {
		return false
	}
	if effort == ReasoningAuto {
		return true
	}
	return slices.Contains(c.Efforts, effort)
}

// Values returns the command/config values in display order, including auto.
func (c ReasoningCapability) Values() []string {
	values := make([]string, 1, len(c.Efforts)+1)
	values[0] = "auto"
	return append(values, c.Efforts...)
}

// ReasoningCapabilityFor resolves the values accepted by the configured
// model. Exact public endpoint/model metadata wins when available; local
// fallback matching is case-insensitive and permits provider-prefixed IDs.
func ReasoningCapabilityFor(model ModelConfig) ReasoningCapability {
	profile, _ := findModelProfile(model.Model)
	known := profile.reasoning
	thinkingMode := known.openAIThinking
	if strings.EqualFold(strings.TrimSpace(model.Protocol), "anthropic") {
		thinkingMode = known.anthropicThinking
	}
	capability := ReasoningCapability{
		Source:  "model",
		Efforts: append([]string(nil), known.efforts...), DisableEffort: known.disableEffort,
		ThinkingMode: thinkingMode, Replay: known.replay,
	}
	var metadata *modelcatalog.Reasoning
	if limits, ok := model.catalogLimits(); ok && limits.Reasoning != nil {
		metadata = limits.Reasoning
		capability.Source = "catalog"
	} else if origin, ok := model.catalog.OriginReasoning(model.Model); ok {
		metadata = origin.Reasoning
		capability.Source = "origin"
		capability.Reference = origin.Provider + "/" + origin.ModelID
	}
	if metadata != nil {
		capability.Efforts = nil
		if !metadata.Supported {
			capability.DisableEffort = ""
			capability.ThinkingMode = ""
			return capability
		}
		// Keep local wire switches and replay policy; the catalog supplies only
		// exact endpoint/model capabilities, never protocol body fragments.
		capability.Efforts = append([]string(nil), metadata.Efforts...)
		if slices.Contains(capability.Efforts, "none") {
			capability.DisableEffort = "none"
		} else {
			capability.DisableEffort = ""
			if metadata.Toggle && thinkingMode != "" {
				capability.Efforts = append([]string{"none"}, capability.Efforts...)
			}
		}
	}
	return capability
}

// ResolveReasoning validates and translates one model's portable setting into
// the provider-facing reasoning contract.
func ResolveReasoning(model ModelConfig) (reasoning.Settings, bool) {
	requested, ok := ParseReasoningEffort(model.ReasoningEffort)
	capability := ReasoningCapabilityFor(model)
	if !ok || !capability.Supports(requested) {
		return reasoning.Settings{}, false
	}
	return reasoning.Settings{
		Requested:     requested,
		Effort:        requested,
		Disabled:      requested == "none",
		DisableEffort: capability.DisableEffort,
		ThinkingMode:  capability.ThinkingMode,
		Replay:        capability.Replay,
	}, true
}

// ReferenceNotice is informational; inferred capability is never authority to
// replay a rejected model request.
func (c ReasoningCapability) ReferenceNotice() string {
	if c.Source == "origin" {
		return "原厂参考（" + c.Reference + "），当前服务未确认"
	}
	return ""
}
