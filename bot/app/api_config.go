package app

import (
	"nekocode/bot/config"
)

type ConfigSnapshot struct {
	Path           string                  `json:"path"`
	Exists         bool                    `json:"exists"`
	Active         string                  `json:"active"`
	ContextWindow  int                     `json:"context_window"`
	FlashModel     string                  `json:"flash_model,omitempty"`
	Models         []config.ModelConfig    `json:"models"`
	ImageGenModels []config.ImageGenConfig `json:"image_gen_models,omitempty"`
}

func (b *Bot) ConfigSnapshot() ConfigSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return configSnapshot(*b.cfg)
}

func (b *Bot) ApplyConfig(snapshot ConfigSnapshot) (ConfigSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	next := config.Config{
		Active:         snapshot.Active,
		ContextWindow:  snapshot.ContextWindow,
		FlashModel:     snapshot.FlashModel,
		Models:         snapshot.Models,
		ImageGenModels: snapshot.ImageGenModels,
	}
	if err := config.Validate(&next); err != nil {
		return ConfigSnapshot{}, err
	}
	if err := config.Save(next); err != nil {
		return ConfigSnapshot{}, err
	}

	oldPrompt, oldCompl := b.ag.TokenUsage()
	b.cfg = &next
	b.initToolRegistry()
	b.initHooks()
	b.initPlugins()
	b.initSkills()
	b.initAgent()
	b.initSummarizer()
	b.initCommands()
	b.ag.AddTokens(oldPrompt, oldCompl)

	return configSnapshot(next), nil
}

func configSnapshot(cfg config.Config) ConfigSnapshot {
	return ConfigSnapshot{
		Path:           config.Path(),
		Exists:         config.Exists(),
		Active:         cfg.Active,
		ContextWindow:  cfg.ContextWindow,
		FlashModel:     cfg.FlashModel,
		Models:         cfg.Models,
		ImageGenModels: cfg.ImageGenModels,
	}
}
