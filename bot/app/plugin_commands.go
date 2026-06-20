package app

import (
	"fmt"

	"nekocode/bot/command"
	"nekocode/bot/plugincli"
)

func (b *Bot) registerPluginCommands() {
	b.cmdParser.Register("plugin", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) == 0 {
			return plugincli.Usage(), true
		}
		switch cmd.Args[0] {
		case "install":
			return b.pluginInstall(cmd.Args[1:])
		case "uninstall":
			return b.pluginUninstall(cmd.Args[1:])
		case "list":
			return b.pluginList(cmd.Args[1:])
		case "enable":
			return b.pluginEnable(cmd.Args[1:])
		case "disable":
			return b.pluginDisable(cmd.Args[1:])
		case "info":
			return b.pluginInfo(cmd.Args[1:])
		default:
			return fmt.Sprintf("Unknown subcommand: %s\n%s", cmd.Args[0], plugincli.Usage()), true
		}
	})
}
