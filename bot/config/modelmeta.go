package config

import (
	"strings"

	"nekocode/bot/reasoning"
)

type reasoningProfile struct {
	efforts           []string
	disableEffort     string
	openAIThinking    string
	anthropicThinking string
	replay            reasoning.ReplayPolicy
}

// modelProfile is the single built-in source for model-dependent behavior.
// First match wins, so exact generations and variants precede family defaults.
// An explicit per-model context_window still overrides this metadata.
type modelProfile struct {
	match         string
	contextWindow int
	reasoning     reasoningProfile
	// vision reports whether the model accepts native image input. It is
	// only a default: an explicit per-model vision config overrides it.
	vision bool
}

var (
	deepSeekReasoning = reasoningProfile{
		efforts: []string{"none", "low", "high", "max"}, disableEffort: "none",
		openAIThinking: "enabled", anthropicThinking: "enabled", replay: reasoning.ReplayToolCalls,
	}
	claudeAdaptiveXHigh = reasoningProfile{
		efforts:           []string{"low", "medium", "high", "xhigh", "max"},
		anthropicThinking: "adaptive", replay: reasoning.ReplaySigned,
	}
	claudeAdaptive = reasoningProfile{
		efforts:           []string{"low", "medium", "high", "max"},
		anthropicThinking: "adaptive", replay: reasoning.ReplaySigned,
	}
	claudeLegacy = reasoningProfile{
		efforts: []string{"low", "medium", "high"}, replay: reasoning.ReplaySigned,
	}
	openAINoneToXHigh = reasoningProfile{
		efforts: []string{"none", "low", "medium", "high", "xhigh"}, disableEffort: "none",
	}
	standardReasoning = reasoningProfile{efforts: []string{"low", "medium", "high"}}
	geminiReasoning   = reasoningProfile{efforts: []string{"minimal", "low", "medium", "high"}}
	// glmThinking: GLM-4.6+ expose a thinking switch rather than effort
	// levels. Declaring the mode lets SetDisableThinking actually emit
	// thinking:{"type":"disabled"} — without it, compaction summaries on
	// GLM burn their 2000-token budget on reasoning and come back empty.
	glmThinking = reasoningProfile{openAIThinking: "enabled"}
)

var knownModelProfiles = []modelProfile{
	// DeepSeek: V4 generation (Pro/Flash) is 1M; the retired deepseek-chat /
	// deepseek-reasoner endpoints were aliases of V4-Flash.
	{match: "deepseek-flash", contextWindow: 1048576, reasoning: deepSeekReasoning},
	{match: "deepseek-v4-flash", contextWindow: 1048576, reasoning: deepSeekReasoning},
	{match: "deepseek-v4-pro", contextWindow: 1048576, reasoning: deepSeekReasoning},
	{match: "deepseek-v4", contextWindow: 1048576, reasoning: deepSeekReasoning},
	{match: "deepseek-chat", contextWindow: 1048576, reasoning: deepSeekReasoning},
	{match: "deepseek-reasoner", contextWindow: 1048576, reasoning: deepSeekReasoning},
	// Anthropic: Opus 4.6+ / Sonnet 4.6+ / Sonnet 5 / Fable are 1M;
	// Haiku and older generations are 200K. All Claude 3+ models accept
	// native image input.
	{match: "claude-opus-5", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh, vision: true},
	{match: "claude-sonnet-5", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh, vision: true},
	{match: "claude-fable-5", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh, vision: true},
	{match: "claude-opus-4-8", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh, vision: true},
	{match: "claude-opus-4-7", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh, vision: true},
	{match: "claude-opus-4-6", contextWindow: 1048576, reasoning: claudeAdaptive, vision: true},
	{match: "claude-sonnet-4-6", contextWindow: 1048576, reasoning: claudeAdaptive, vision: true},
	{match: "claude-opus-4-5", contextWindow: 200000, reasoning: claudeLegacy, vision: true},
	{match: "claude-haiku", contextWindow: 200000, vision: true},
	{match: "claude", contextWindow: 200000, vision: true},
	// OpenAI: 5.4+ are 1.05M; 5/5.1/5.2 are 400K. GPT-4o/4.1/5.x and the
	// o-series reasoning models accept native image input.
	{match: "gpt-5.6", contextWindow: 1050000, reasoning: reasoningProfile{efforts: []string{"none", "low", "medium", "high", "xhigh", "max"}, disableEffort: "none"}, vision: true},
	{match: "gpt-5.5", contextWindow: 1050000, reasoning: openAINoneToXHigh, vision: true},
	{match: "gpt-5.4", contextWindow: 1050000, reasoning: openAINoneToXHigh, vision: true},
	{match: "gpt-5.2", contextWindow: 400000, reasoning: openAINoneToXHigh, vision: true},
	{match: "gpt-5.1", contextWindow: 400000, reasoning: reasoningProfile{efforts: []string{"none", "low", "medium", "high"}, disableEffort: "none"}, vision: true},
	{match: "gpt-5-pro", contextWindow: 400000, reasoning: reasoningProfile{efforts: []string{"high"}}, vision: true},
	{match: "gpt-5", contextWindow: 400000, reasoning: reasoningProfile{efforts: []string{"minimal", "low", "medium", "high"}}, vision: true},
	// o-series: the full o1/o3 and o4-mini models accept native image
	// input; the mini/preview variants are text-only and must be matched
	// before the family prefixes, or a pasted image would be delivered
	// natively and fail with an API 400.
	{match: "o1-mini", contextWindow: 128000, reasoning: standardReasoning},
	{match: "o1-preview", contextWindow: 128000, reasoning: standardReasoning},
	{match: "o1-", contextWindow: 200000, reasoning: standardReasoning, vision: true},
	{match: "o3-mini", contextWindow: 200000, reasoning: standardReasoning},
	{match: "o3-", contextWindow: 200000, reasoning: standardReasoning, vision: true},
	{match: "o4-mini", contextWindow: 200000, reasoning: standardReasoning, vision: true},
	{match: "gpt-4.1", contextWindow: 1047576, vision: true},
	{match: "gpt-4o", contextWindow: 128000, vision: true},
	{match: "gpt-4-turbo", contextWindow: 128000, vision: true},
	// Zhipu GLM: 5.x (including flash variants) is 1M; 4.6 is 200K; older
	// 4.x is 128K. GLM-5 and the GLM-4V series accept native image input.
	{match: "glm-5", contextWindow: 1048576, reasoning: glmThinking, vision: true},
	{match: "glm-4v", contextWindow: 131072, vision: true},
	{match: "glm-4.6", contextWindow: 200000, reasoning: glmThinking},
	{match: "glm-4", contextWindow: 131072},
	// Moonshot / Kimi: K3 is 1M; K2.x is 256K; legacy moonshot-v1 varies.
	{match: "kimi-k3", contextWindow: 1048576},
	{match: "moonshot-v1-8k", contextWindow: 8192},
	{match: "moonshot-v1-32k", contextWindow: 32768},
	{match: "moonshot-v1-128k", contextWindow: 131072},
	{match: "kimi", contextWindow: 262144},
	// Alibaba Qwen. The VL series accepts native image input and must be
	// matched before the plain text families.
	{match: "qwen3-vl", contextWindow: 131072, vision: true},
	{match: "qwen2.5-vl", contextWindow: 131072, vision: true},
	{match: "qwen-vl", contextWindow: 131072, vision: true},
	{match: "qwen3", contextWindow: 131072},
	{match: "qwen2.5", contextWindow: 131072},
	{match: "qwq", contextWindow: 131072},
	// Google. All Gemini models accept native image input.
	{match: "gemini-3", contextWindow: 1048576, reasoning: geminiReasoning, vision: true},
	{match: "gemini-2.5-pro", contextWindow: 1048576, reasoning: geminiReasoning, vision: true},
	{match: "gemini-2.5-flash", contextWindow: 1048576, reasoning: reasoningProfile{efforts: []string{"none", "minimal", "low", "medium", "high"}, disableEffort: "none"}, vision: true},
	{match: "gemini", contextWindow: 1048576, vision: true},
	// xAI: Grok 4.1 Fast up to 2M; 4.5 is 500K; 4/4.1 is 256K; 3 is 128K.
	{match: "grok-4.1-fast", contextWindow: 2097152},
	{match: "grok-4.5", contextWindow: 512000},
	{match: "grok-4", contextWindow: 262144},
	{match: "grok-3", contextWindow: 131072},
	// Meta: Llama 4 Scout 10M, Maverick 1M; 3.x is 128K.
	{match: "llama-4-scout", contextWindow: 10485760},
	{match: "llama-4", contextWindow: 1048576},
	{match: "llama-3.1", contextWindow: 131072},
	{match: "llama-3.3", contextWindow: 131072},
	// MiniMax
	{match: "minimax-text-01", contextWindow: 1048576},
	// Mistral
	{match: "mistral-large", contextWindow: 131072},
}

func findModelProfile(model string) (modelProfile, bool) {
	id := strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndexByte(id, '/'); slash >= 0 {
		id = id[slash+1:]
	}
	for _, profile := range knownModelProfiles {
		if strings.HasPrefix(id, profile.match) {
			return profile, true
		}
	}
	return modelProfile{}, false
}

// KnownContextWindow reports the built-in context window for a model ID
// (matched case-insensitively against the provider-stripped model ID, so "deepseek/deepseek-v4-pro"
// style prefixed IDs also hit). ok is false for unknown models.
func KnownContextWindow(model string) (window int, ok bool) {
	if profile, found := findModelProfile(model); found && profile.contextWindow > 0 {
		return profile.contextWindow, true
	}
	return 0, false
}

// KnownVision reports whether the built-in model table knows the model to
// accept native image input. Unknown models report false; an explicit
// per-model vision config overrides this.
func KnownVision(model string) bool {
	if profile, found := findModelProfile(model); found {
		return profile.vision
	}
	return false
}

// EffectiveVision resolves whether a model receives native image input: an
// explicit config value wins, then the built-in model table, then false.
func (m ModelConfig) EffectiveVision() bool {
	if m.Vision != nil {
		return *m.Vision
	}
	return KnownVision(m.Model)
}
