package app

import (
	"nekocode/bot/config"
)

func (b *Bot) ConfigView() config.View {
	b.mu.Lock()
	defer b.mu.Unlock()
	return config.NewView(*b.cfg)
}

func (b *Bot) ApplyConfig(view config.View) (config.View, error) {
	next := view.Config()
	if err := config.Validate(&next); err != nil {
		return config.View{}, err
	}
	if err := config.Save(next); err != nil {
		return config.View{}, err
	}

	b.mu.Lock()
	oldPrompt, oldCompl := 0, 0
	if b.ag != nil {
		oldPrompt, oldCompl = b.ag.TokenUsage()
	}
	b.cfg = &next
	b.mu.Unlock()

	go b.reloadRuntime(oldPrompt, oldCompl)

	return config.NewView(next), nil
}

func (b *Bot) reloadRuntime(oldPrompt, oldCompl int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reinit()
	if b.ag != nil {
		b.ag.AddTokens(oldPrompt, oldCompl)
	}
}
