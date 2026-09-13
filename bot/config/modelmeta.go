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
	// Haiku and older generations are 200K.
	{match: "claude-opus-5", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh},
	{match: "claude-sonnet-5", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh},
	{match: "claude-fable-5", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh},
	{match: "claude-opus-4-8", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh},
	{match: "claude-opus-4-7", contextWindow: 1048576, reasoning: claudeAdaptiveXHigh},
	{match: "claude-opus-4-6", contextWindow: 1048576, reasoning: claudeAdaptive},
	{match: "claude-sonnet-4-6", contextWindow: 1048576, reasoning: claudeAdaptive},
	{match: "claude-opus-4-5", contextWindow: 200000, reasoning: claudeLegacy},
	{match: "claude-haiku", contextWindow: 200000},
	{match: "claude", contextWindow: 200000},
	// OpenAI: 5.4+ are 1.05M; 5/5.1/5.2 are 400K.
	{match: "gpt-5.6", contextWindow: 1050000, reasoning: reasoningProfile{efforts: []string{"none", "low", "medium", "high", "xhigh", "max"}, disableEffort: "none"}},
	{match: "gpt-5.5", contextWindow: 1050000, reasoning: openAINoneToXHigh},
	{match: "gpt-5.4", contextWindow: 1050000, reasoning: openAINoneToXHigh},
	{match: "gpt-5.2", contextWindow: 400000, reasoning: openAINoneToXHigh},
	{match: "gpt-5.1", contextWindow: 400000, reasoning: reasoningProfile{efforts: []string{"none", "low", "medium", "high"}, disableEffort: "none"}},
	{match: "gpt-5-pro", contextWindow: 400000, reasoning: reasoningProfile{efforts: []string{"high"}}},
	{match: "gpt-5", contextWindow: 400000, reasoning: reasoningProfile{efforts: []string{"minimal", "low", "medium", "high"}}},
	{match: "o1-", contextWindow: 200000, reasoning: standardReasoning},
	{match: "o3-", contextWindow: 200000, reasoning: standardReasoning},
	{match: "o4-mini", contextWindow: 200000, reasoning: standardReasoning},
	{match: "gpt-4.1", contextWindow: 1047576},
	{match: "gpt-4o", contextWindow: 128000},
	{match: "gpt-4-turbo", contextWindow: 128000},
	// Zhipu GLM: 5.x and 4.6 are 200K; older 4.x is 128K.
	{match: "glm-5", contextWindow: 200000},
	{match: "glm-4.6", contextWindow: 200000},
	{match: "glm-4", contextWindow: 131072},
	// Moonshot / Kimi: K3 is 1M; K2.x is 256K; legacy moonshot-v1 varies.
	{match: "kimi-k3", contextWindow: 1048576},
	{match: "moonshot-v1-8k", contextWindow: 8192},
	{match: "moonshot-v1-32k", contextWindow: 32768},
	{match: "moonshot-v1-128k", contextWindow: 131072},
	{match: "kimi", contextWindow: 262144},
	// Alibaba Qwen
	{match: "qwen3", contextWindow: 131072},
	{match: "qwen2.5", contextWindow: 131072},
	{match: "qwq", contextWindow: 131072},
	// Google
	{match: "gemini-3", contextWindow: 1048576, reasoning: geminiReasoning},
	{match: "gemini-2.5-pro", contextWindow: 1048576, reasoning: geminiReasoning},
	{match: "gemini-2.5-flash", contextWindow: 1048576, reasoning: reasoningProfile{efforts: []string{"none", "minimal", "low", "medium", "high"}, disableEffort: "none"}},
	{match: "gemini", contextWindow: 1048576},
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
