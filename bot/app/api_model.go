package app

import "fmt"

func (b *Bot) SwitchModel(name string) (string, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.cfg.SwitchModel(name) {
		return "", "", fmt.Errorf("model %q not found. Available: %v", name, b.cfg.AllModelNames())
	}

	oldPrompt, oldCompl := b.ag.TokenUsage()
	b.initAgent()
	b.ag.AddTokens(oldPrompt, oldCompl)
	b.ctxMgr.ResetCache()

	am := b.cfg.ActiveModelConfig()
	return am.Model, am.Provider, nil
}
