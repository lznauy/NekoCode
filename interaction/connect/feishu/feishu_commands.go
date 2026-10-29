package feishu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nekocode/interaction/connect"
	"nekocode/protocol"
)

const usageAdd = "Usage: /connect feishu add <app-id> <app-secret>"

func (c *Connector) CommandMenu(_ context.Context, args []string) (protocol.CommandMenu, bool) {
	return connect.ConnectorCommandMenu("feishu", args, nil)
}

func (c *Connector) HandleCommand(ctx context.Context, args []string) (string, error) {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "connect":
			return c.connect(ctx)
		case "add":
			return c.addApp(args[1:])
		case "pair":
			return c.pair(ctx)
		case "unpair":
			return c.unpair()
		case "reset":
			return c.reset()
		case "status":
			return c.status()
		case "disconnect", "stop":
			if err := c.Stop(); err != nil {
				return "", err
			}
			return "Feishu connector stopped.", nil
		default:
			return fmt.Sprintf("Unknown Feishu command %q.\nUsage: /connect feishu <connect|add|pair|unpair|reset|status|disconnect>", args[0]), nil
		}
	}
	return c.connect(ctx)
}

// connect is the bare "/connect feishu": setup help, pairing, or start.
func (c *Connector) connect(ctx context.Context) (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if !cfg.configured() {
		return setupInstructions(), nil
	}
	if cfg.Owner == nil {
		return c.pair(ctx)
	}
	if err := c.Start(ctx); err != nil {
		return "", err
	}
	return fmt.Sprintf("Feishu connected.\n\nApp: %s\nOwner: %s", cfg.AppID, cfg.Owner.UserID), nil
}

func (c *Connector) addApp(args []string) (string, error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return usageAdd, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	nextAppID := strings.TrimSpace(args[0])
	if cfg.AppID != "" && cfg.AppID != nextAppID {
		cfg.unpair()
	}
	cfg.AppID = nextAppID
	cfg.AppSecret = strings.TrimSpace(args[1])
	if err := saveConfig(cfg); err != nil {
		return "", err
	}
	return "Feishu app saved.\nRun /connect feishu pair to bind your Feishu account.", nil
}

// pair starts (or restarts) the pairing flow: a fresh nonce is generated,
// the connector starts listening, and the user DM's the code to the bot.
func (c *Connector) pair(ctx context.Context) (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if !cfg.configured() {
		return setupInstructions(), nil
	}
	if cfg.Owner != nil {
		return fmt.Sprintf("Feishu is already paired with %s.\nRun /connect feishu unpair before pairing another account.", cfg.Owner.UserID), nil
	}
	nonce, err := connect.NewNonce(9)
	if err != nil {
		return "", err
	}
	cfg.Start(nonce, time.Now(), pairingTTL)
	if err := saveConfig(cfg); err != nil {
		return "", err
	}
	if err := c.Start(ctx); err != nil {
		return "", err
	}
	return fmt.Sprintf(`Feishu pairing started.

Send this pairing code to the bot in a Feishu direct message:

%s

Pairing expires in %d minutes.
After pairing, Feishu messages will be routed into this NekoCode session.`, nonce, int(pairingTTL/time.Minute)), nil
}

func (c *Connector) unpair() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	cfg.unpair()
	if err := saveConfig(cfg); err != nil {
		return "", err
	}
	return "Feishu unpaired.", nil
}

func (c *Connector) reset() (string, error) {
	if c.base.IsRunning() {
		if err := c.Stop(); err != nil {
			return "", err
		}
	}
	if err := saveConfig(Config{}); err != nil {
		return "", err
	}
	return "Feishu configuration reset.", nil
}

func (c *Connector) status() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if !cfg.configured() {
		return "Feishu is not configured.\n\n" + setupInstructions(), nil
	}
	owner := "unpaired"
	if cfg.Owner != nil {
		owner = cfg.Owner.UserID
	}
	return fmt.Sprintf("Feishu: running=%v app=%s owner=%s", c.base.IsRunning(), cfg.AppID, owner), nil
}
