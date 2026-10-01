// tui.go — package tui 入口。
package tui

import (
	"errors"
	"fmt"

	"nekocode/interaction/tui/components/message"
	"nekocode/runtime/standard"

	tea "charm.land/bubbletea/v2"
)

func Run() (err error) {
	message.Warmup()
	rt, err := standard.New()
	if err != nil {
		return fmt.Errorf("initialize runtime: %w", err)
	}
	defer func() {
		err = errors.Join(err, rt.Close())
	}()
	model, err := NewModel(rt)
	if err != nil {
		return err
	}
	defer model.Input.Clear()
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
