package core

import (
	"errors"

	"nekocode/mode"
)

func (b *Bot) InteractionMode() mode.ID {
	if id := b.interactionMode.Load(); id != nil && *id != "" {
		return mode.ID(*id)
	}
	return mode.Standard
}

func (b *Bot) storeInteractionMode(id string)      { b.interactionMode.Store(&id) }
func (b *Bot) InteractionModes() []mode.Definition { return mode.Definitions() }

// SetInteractionMode is the external mutation boundary. Commands already hold
// operationMu and use the internal setter instead of recursively acquiring it.
func (b *Bot) SetInteractionMode(id mode.ID) error {
	if !b.operationMu.TryLock() {
		return errors.New("bot: another operation is running")
	}
	defer b.operationMu.Unlock()
	previous := b.InteractionMode()
	if err := b.setInteractionMode(id); err != nil {
		return err
	}
	if err := b.saveSession(); err != nil {
		b.storeInteractionMode(string(previous))
		return err
	}
	return nil
}

func (b *Bot) setInteractionMode(id mode.ID) error {
	d, err := mode.Resolve(id)
	if err != nil {
		return err
	}
	b.storeInteractionMode(string(d.ID))
	return nil
}

func (b *Bot) ResolveInteractionMode(input string) (mode.Snapshot, error) {
	id := b.InteractionMode()
	if mode.OneShot(input) {
		id = mode.Plan
	}
	d, err := mode.Resolve(id)
	if err != nil {
		return mode.Snapshot{}, err
	}
	if _, err := interactionStrategy(d.Strategy); err != nil {
		return mode.Snapshot{}, err
	}
	return d.Snapshot, nil
}
