package qqbot

import (
	"fmt"
	"strings"

	"nekocode/interaction/connect"
)

const section = "qqbot"

// Config 是 connect.json 中 "qqbot" 节的持久化配置。
type Config struct {
	snapshot  *connect.SectionVersion
	AppID     string `json:"app_id,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
	Sandbox   bool   `json:"sandbox,omitempty"`
}

func (c Config) configured() bool {
	return strings.TrimSpace(c.AppID) != "" && strings.TrimSpace(c.AppSecret) != ""
}

func configPath() string { return connect.DefaultFileStore().Path() }

func loadConfig() (Config, error) {
	var cfg Config
	version, err := connect.DefaultFileStore().LoadVersioned(section, &cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.snapshot = version
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.AppSecret = strings.TrimSpace(cfg.AppSecret)
	return cfg, nil
}

func saveConfig(cfg Config) error {
	return connect.DefaultFileStore().SaveVersioned(section, cfg, cfg.snapshot)
}

func setupInstructions() string {
	return fmt.Sprintf(`QQBot is not configured.

1. 在 QQ 机器人开放平台（q.qq.com）创建机器人，拿到 AppID 和 AppSecret。
2. 在这里配置：

/connect qqbot add <appid> <appsecret>

沙箱环境可用 /connect qqbot sandbox on 切换。

Config path: %s`, configPath())
}
