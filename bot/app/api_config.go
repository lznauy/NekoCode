package app

import (
	"nekocode/bot/config"
)

func (b *Bot) ConfigSnapshot() config.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return config.NewSnapshot(*b.cfg)
}

func (b *Bot) ApplyConfig(snapshot config.Snapshot) (config.Snapshot, error) {
	next := snapshot.Config()
	if err := config.Validate(&next); err != nil {
		return config.Snapshot{}, err
	}
	if err := config.Save(next); err != nil {
		return config.Snapshot{}, err
	}

	b.mu.Lock()
	oldPrompt, oldCompl := 0, 0
	if b.ag != nil {
		oldPrompt, oldCompl = b.ag.TokenUsage()
	}
	b.cfg = &next
	b.mu.Unlock()

	go b.reloadRuntime(oldPrompt, oldCompl)

	return config.NewSnapshot(next), nil
}

func (b *Bot) reloadRuntime(oldPrompt, oldCompl int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reinit()
	if b.ag != nil {
		b.ag.AddTokens(oldPrompt, oldCompl)
	}
}
