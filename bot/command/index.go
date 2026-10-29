package command

import (
	"context"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/protocol"
	"strings"
)

func registerIndex(p *Parser, deps Deps) {
	p.RegisterInfo("index", "Inspect or rebuild the project index", func(ctx context.Context, cmd *Command) (string, bool) {
		action := "status"
		if len(cmd.Args) > 1 {
			return "Usage: /index [status|rebuild]", true
		}
		if len(cmd.Args) == 1 {
			action = strings.ToLower(cmd.Args[0])
		}
		if action != "status" && action != "rebuild" {
			return "Usage: /index [status|rebuild]", true
		}
		if deps.ToolRegistry == nil {
			return "Project index is unavailable.", true
		}
		tool, err := deps.ToolRegistry.Get("index")
		if err != nil {
			return "Project index is unavailable.", true
		}
		maintainer, ok := tool.(interface {
			Maintain(context.Context, string) (string, error)
		})
		if !ok {
			return "Project index maintenance is unavailable.", true
		}
		if ws := deps.ToolRegistry.Workspace(); ws != nil {
			ctx = workspace.WithManager(ctx, ws)
		} else {
			return "Project index workspace is unavailable.", true
		}
		result, err := maintainer.Maintain(ctx, action)
		if err != nil {
			return "Index maintenance failed: " + err.Error(), true
		}
		return result, true
	})
	p.RegisterMenu("index", func(_ context.Context, cmd *Command) (protocol.CommandMenu, bool) {
		if len(cmd.Args) != 0 {
			return protocol.CommandMenu{}, false
		}
		return protocol.CommandMenu{Title: "Project index", Items: []protocol.CommandMenuItem{
			{Key: "status", Value: "/index status", Label: "Status", Description: "Show files, languages, last sync and watcher health", Submit: true},
			{Key: "rebuild", Value: "/index rebuild", Label: "Rebuild", Description: "Back up and rebuild the project index", Submit: true},
		}}, true
	})
}
