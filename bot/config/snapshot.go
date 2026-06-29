package config

type Snapshot struct {
	Path           string           `json:"path"`
	Exists         bool             `json:"exists"`
	Active         string           `json:"active"`
	ContextWindow  int              `json:"context_window"`
	FlashModel     string           `json:"flash_model,omitempty"`
	Models         []ModelConfig    `json:"models"`
	ImageGenModels []ImageGenConfig `json:"image_gen_models,omitempty"`
}

func NewSnapshot(cfg Config) Snapshot {
	return Snapshot{
		Path:           Path(),
		Exists:         Exists(),
		Active:         cfg.Active,
		ContextWindow:  cfg.ContextWindow,
		FlashModel:     cfg.FlashModel,
		Models:         cfg.Models,
		ImageGenModels: cfg.ImageGenModels,
	}
}

func (s Snapshot) Config() Config {
	return Config{
		Active:         s.Active,
		ContextWindow:  s.ContextWindow,
		FlashModel:     s.FlashModel,
		Models:         s.Models,
		ImageGenModels: s.ImageGenModels,
	}
}
