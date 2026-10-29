package wecom

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nekocode/interaction/connect"
	"nekocode/protocol"
)

const usageAdd = "Usage: /connect wecom add <bot-id> <secret>"

func (c *Connector) CommandMenu(_ context.Context, args []string) (protocol.CommandMenu, bool) {
	return connect.ConnectorCommandMenu("wecom", args, nil)
}

func (c *Connector) HandleCommand(ctx context.Context, args []string) (string, error) {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "connect":
			return c.connect(ctx)
		case "add":
			return c.add(ctx, args[1:])
		case "pair":
			return c.pair(ctx)
		case "unpair":
			return c.unpair()
		case "reset":
			return c.reset()
		case "start":
			if err := c.Start(ctx); err != nil {
				return "", err
			}
			return "WeCom connector started.", nil
		case "stop", "disconnect":
			if err := c.Stop(); err != nil {
				return "", err
			}
			return "WeCom connector stopped.", nil
		case "status":
			return c.status()
		default:
			return fmt.Sprintf("Unknown WeCom command %q.\nUsage: /connect wecom <connect|add|pair|unpair|reset|start|stop|status>", args[0]), nil
		}
	}
	return c.connect(ctx)
}

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
	return fmt.Sprintf("WeCom connected.\n\nBot: %s\nOwner: %s", cfg.BotID, cfg.Owner.UserID), nil
}

func (c *Connector) add(ctx context.Context, args []string) (string, error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return usageAdd, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	nextBotID := strings.TrimSpace(args[0])
	if cfg.BotID != "" && cfg.BotID != nextBotID {
		cfg.unpair()
		c.clearDeliveryState()
	}
	cfg.BotID = nextBotID
	cfg.Secret = strings.TrimSpace(args[1])
	if err := saveConfig(cfg); err != nil {
		return "", err
	}
	if c.base.IsRunning() {
		_ = c.Stop()
	}
	if err := c.Start(ctx); err != nil {
		return "", err
	}
	return "WeCom 凭证已保存，正在连接。\n运行 /connect wecom pair 绑定你的企业微信账号。", nil
}

func (c *Connector) pair(ctx context.Context) (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if !cfg.configured() {
		return setupInstructions(), nil
	}
	if cfg.Owner != nil {
		return fmt.Sprintf("WeCom 已与 %s 配对。请先运行 /connect wecom unpair 再绑定其他账号。", cfg.Owner.UserID), nil
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
	return fmt.Sprintf(`WeCom pairing started.

请在企业微信中把下面的配对码发送给智能机器人：

%s

配对码将在 %d 分钟后过期。`, nonce, int(pairingTTL/time.Minute)), nil
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
	c.clearDeliveryState()
	return "WeCom unpaired.", nil
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
	c.clearDeliveryState()
	return "WeCom configuration reset.", nil
}

func (c *Connector) status() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if !cfg.configured() {
		return "WeCom is not configured.\n\n" + setupInstructions(), nil
	}
	owner := "unpaired"
	if cfg.Owner != nil {
		owner = cfg.Owner.UserID
	}
	return fmt.Sprintf("WeCom: running=%v bot=%s owner=%s", c.base.IsRunning(), cfg.BotID, owner), nil
}
