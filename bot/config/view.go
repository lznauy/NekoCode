package config

type View struct {
	Path           string                     `json:"path"`
	Exists         bool                       `json:"exists"`
	Active         string                     `json:"active"`
	ContextWindow  int                        `json:"context_window"`
	FlashModel     string                     `json:"flash_model,omitempty"`
	Models         []ModelConfig              `json:"models"`
	ImageGenModels []ImageGenConfig           `json:"image_gen_models,omitempty"`
	MCPServers     map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
}

func NewView(cfg Config) View {
	return View{
		Path:           Path(),
		Exists:         Exists(),
		Active:         cfg.Active,
		ContextWindow:  cfg.ContextWindow,
		FlashModel:     cfg.FlashModel,
		Models:         cfg.Models,
		ImageGenModels: cfg.ImageGenModels,
		MCPServers:     cfg.MCPServers,
	}
}

func (v View) Config() Config {
	return Config{
		Active:         v.Active,
		ContextWindow:  v.ContextWindow,
		FlashModel:     v.FlashModel,
		Models:         v.Models,
		ImageGenModels: v.ImageGenModels,
		MCPServers:     v.MCPServers,
	}
}
