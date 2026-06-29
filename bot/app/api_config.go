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
	b.mu.Lock()
	defer b.mu.Unlock()

	next := snapshot.Config()
	if err := config.Validate(&next); err != nil {
		return config.Snapshot{}, err
	}
	if err := config.Save(next); err != nil {
		return config.Snapshot{}, err
	}

	oldPrompt, oldCompl := b.ag.TokenUsage()
	b.cfg = &next
	b.reinit()
	b.ag.AddTokens(oldPrompt, oldCompl)

	return config.NewSnapshot(next), nil
}
