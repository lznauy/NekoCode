package qqbot

import (
	"context"
	"fmt"
	"strings"

	"nekocode/interaction/connect"
	"nekocode/protocol"
)

const (
	usageAdd     = "Usage: /connect qqbot add <appid> <appsecret>"
	usageSandbox = "Usage: /connect qqbot sandbox on|off"
)

const usage = `Usage: /connect qqbot <command>
Commands:
  add <appid> <appsecret>  保存 QQ 机器人开放平台凭证并启动连接
  sandbox on|off           切换沙箱 / 生产环境
  start                    启动连接
  stop | disconnect        断开连接
  status                   查看状态

在 q.qq.com 创建机器人可获取 AppID 和 AppSecret。`

func (c *Connector) CommandMenu(_ context.Context, args []string) (protocol.CommandMenu, bool) {
	return connect.ConnectorCommandMenu("qqbot", args, nil)
}

func (c *Connector) HandleCommand(ctx context.Context, args []string) (string, error) {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "add":
			return c.add(ctx, args[1:])
		case "sandbox":
			return c.setSandbox(ctx, args[1:])
		case "start":
			if err := c.Start(ctx); err != nil {
				return "", err
			}
			return "QQBot connector started.", nil
		case "stop", "disconnect":
			if err := c.Stop(); err != nil {
				return "", err
			}
			return "QQBot connector stopped.", nil
		case "status":
			return c.status()
		}
	}
	// 无参数：用法 + 当前状态。
	status, _ := c.status()
	return usage + "\n\n" + status, nil
}

// add 保存凭证并启动连接；凭证变更时先停掉旧连接。
func (c *Connector) add(ctx context.Context, args []string) (string, error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return usageAdd, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	cfg.AppID = strings.TrimSpace(args[0])
	cfg.AppSecret = strings.TrimSpace(args[1])
	if err := saveConfig(cfg); err != nil {
		return "", err
	}
	if c.base.IsRunning() {
		_ = c.Stop()
	}
	if err := c.Start(ctx); err != nil {
		return "", err
	}
	return fmt.Sprintf("QQBot 凭证已保存，正在连接（%s）。", envName(cfg.Sandbox)), nil
}

// setSandbox 切换沙箱环境；运行中时重启连接使新基地址生效。
func (c *Connector) setSandbox(ctx context.Context, args []string) (string, error) {
	if len(args) < 1 {
		return usageSandbox, nil
	}
	var sandbox bool
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "on":
		sandbox = true
	case "off":
		sandbox = false
	default:
		return usageSandbox, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	cfg.Sandbox = sandbox
	if err := saveConfig(cfg); err != nil {
		return "", err
	}
	if c.base.IsRunning() {
		_ = c.Stop()
		if err := c.Start(ctx); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("QQBot 已切换到%s环境。", envName(sandbox)), nil
}

func (c *Connector) status() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if !cfg.configured() {
		return setupInstructions(), nil
	}
	return fmt.Sprintf("QQBot: running=%v app_id=%s env=%s",
		c.base.IsRunning(), cfg.AppID, envName(cfg.Sandbox)), nil
}

func envName(sandbox bool) string {
	if sandbox {
		return "沙箱"
	}
	return "生产"
}
