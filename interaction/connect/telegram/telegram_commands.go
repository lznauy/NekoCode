package telegram

import (
	"context"
	"fmt"
	"strings"

	"nekocode/interaction/connect"
	"nekocode/protocol"
)

const (
	usageAddProfile    = "Usage: /connect telegram add [name] <bot-token>"
	usageUseProfile    = "Usage: /connect telegram use <name>"
	usageRemoveProfile = "Usage: /connect telegram remove <name>"
)

func (c *Connector) CommandMenu(_ context.Context, args []string) (protocol.CommandMenu, bool) {
	cfg, err := loadConfig()
	var profiles []string
	if err == nil {
		for _, profile := range cfg.Profiles {
			profiles = append(profiles, profile.Name)
		}
	}
	return connect.ConnectorCommandMenu("telegram", args, profiles)
}

func (c *Connector) HandleCommand(ctx context.Context, args []string) (string, error) {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "connect":
			return c.connectActive(ctx)
		case "add", "token":
			return c.addProfile(ctx, args[1:])
		case "profiles", "list":
			return c.profiles()
		case "use":
			return c.useProfile(ctx, args[1:])
		case "pair":
			return c.pairProfile(ctx, args[1:])
		case "unpair":
			return c.unpairProfile(args[1:])
		case "remove", "delete":
			return c.removeProfile(args[1:])
		case "reset":
			return c.resetConfig()
		case "status":
			return c.status()
		case "disconnect", "stop":
			if err := c.Stop(); err != nil {
				return "", err
			}
			return "Telegram connector stopped.", nil
		default:
			return fmt.Sprintf("Unknown Telegram command %q.\nUsage: /connect telegram <connect|add|profiles|use|pair|unpair|remove|reset|status|disconnect>", args[0]), nil
		}
	}
	return c.connectActive(ctx)
}
