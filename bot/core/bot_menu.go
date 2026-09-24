package core

import (
	"context"

	"nekocode/bot/command"
	"nekocode/bot/config"
	"nekocode/protocol"
)

func (b *Bot) registerCommandMenus(p *command.Parser) {
	p.RegisterMenu("model", func(_ context.Context, cmd *command.Command) (protocol.CommandMenu, bool) {
		if len(cmd.Args) != 0 {
			return protocol.CommandMenu{}, false
		}
		active := b.cfg.Active
		items := make([]protocol.CommandMenuItem, 0, len(b.cfg.Models))
		for _, model := range b.cfg.Models {
			items = append(items, protocol.CommandMenuItem{
				Value: "/model " + model.Name, Label: model.Name,
				Description: model.Provider + " / " + model.Model,
				Submit:      true, Current: model.Name == active,
			})
		}
		return protocol.CommandMenu{Title: "Choose model", Empty: "No models configured", Items: items}, true
	})

	p.RegisterMenu("permission", func(_ context.Context, cmd *command.Command) (protocol.CommandMenu, bool) {
		if len(cmd.Args) != 0 {
			return protocol.CommandMenu{}, false
		}
		full := b.FullAccess()
		manualDesc := "Prompt for approval on guarded commands (default)"
		fullDesc := "Run ALL commands with no approval — DANGEROUS"
		items := []protocol.CommandMenuItem{
			{Value: "/permission manual", Label: "manual", Description: manualDesc, Submit: true, Current: !full && !b.BashAuto()},
		}
		if b.CanBashAuto() {
			items = append(items, protocol.CommandMenuItem{Value: "/permission auto", Label: "auto", Description: "Jev 优先判定 shell 指令，安全直接执行，可疑弹授权", Submit: true, Current: b.BashAuto()})
		}
		items = append(items, protocol.CommandMenuItem{Value: "/permission full", Label: "full (全接管)", Description: fullDesc, Submit: true, Current: full})
		return protocol.CommandMenu{Title: "Permission mode", Items: items}, true
	})

	p.RegisterMenu("effort", func(_ context.Context, cmd *command.Command) (protocol.CommandMenu, bool) {
		if len(cmd.Args) != 0 {
			return protocol.CommandMenu{}, false
		}
		model := b.cfg.ActiveModelConfig()
		current := model.ReasoningEffort
		levels := config.ReasoningCapabilityFor(model).Values()
		currentValue := current
		if currentValue == "" {
			currentValue = "auto"
		}
		items := make([]protocol.CommandMenuItem, 0, len(levels))
		for _, level := range levels {
			label := level
			switch level {
			case "auto":
				label = "Auto"
			case "none":
				label = "Off"
			}
			items = append(items, protocol.CommandMenuItem{
				Value: "/effort " + level, Label: label,
				Description: reasoningEffortDescription(level),
				Submit:      true, Current: level == currentValue,
			})
		}
		return protocol.CommandMenu{Title: "Reasoning effort", Items: items}, true
	})

	p.RegisterMenu("rewind", func(_ context.Context, cmd *command.Command) (protocol.CommandMenu, bool) {
		if len(cmd.Args) != 0 || b.checkpoints == nil || b.sess == nil {
			return protocol.CommandMenu{}, false
		}
		points, err := b.checkpointInfos()
		if err != nil {
			return protocol.CommandMenu{Title: "Rewind to message", Empty: err.Error()}, true
		}
		items := make([]protocol.CommandMenuItem, 0, len(points))
		for _, point := range points {
			items = append(items, protocol.CommandMenuItem{
				Value: "/rewind " + point.ID, Label: point.Label,
				Description: point.Description, Submit: true,
			})
		}
		return protocol.CommandMenu{Title: "Rewind to message", Empty: "No rewind points available", Items: items}, true
	})
}

func reasoningEffortDescription(effort string) string {
	switch effort {
	case "auto":
		return "Use the provider/model default"
	case "none":
		return "Disable reasoning when supported"
	case "minimal":
		return "Minimum reasoning budget"
	case "low":
		return "Faster, lighter reasoning"
	case "medium":
		return "Balanced reasoning"
	case "high":
		return "Deeper reasoning"
	case "xhigh":
		return "Extra-high reasoning when supported"
	default:
		return "Maximum reasoning when supported"
	}
}
