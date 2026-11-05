package wecom

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nekocode/interaction/connect"
)

const (
	section    = "wecom"
	pairingTTL = connect.DefaultPairingTTL
)

var configMu sync.Mutex
var configRevision uint64
var errConfigChanged = errors.New("configuration changed; retry the command")

type configSnapshot struct {
	value    string
	revision uint64
}

// Config is the persisted configuration for one WeCom intelligent robot.
type Config struct {
	snapshot *configSnapshot
	BotID    string                 `json:"bot_id,omitempty"`
	Secret   string                 `json:"secret,omitempty"`
	Owner    *connect.Owner[string] `json:"owner,omitempty"`
	connect.Pairing
}

func (c Config) configured() bool {
	return strings.TrimSpace(c.BotID) != "" && strings.TrimSpace(c.Secret) != ""
}

func (c Config) isAllowed(userID string) bool { return c.Owner.Matches(userID) }

func (c *Config) finishPairing(userID, chatID string) {
	connect.SetOwner(&c.Owner, userID, "", chatID, time.Now())
	c.Clear()
}

func (c *Config) unpair() {
	c.Owner = nil
	c.Clear()
}

func (c *Config) touchOwner(userID, chatID string) {
	c.Owner.Touch(userID, "", chatID, time.Now())
}

func loadConfig() (Config, error) {
	configMu.Lock()
	defer configMu.Unlock()
	return loadConfigLocked()
}

func loadConfigLocked() (Config, error) {
	var cfg Config
	if err := connect.DefaultFileStore().Load(section, &cfg); err != nil {
		return Config{}, err
	}
	cfg.BotID = strings.TrimSpace(cfg.BotID)
	cfg.Secret = strings.TrimSpace(cfg.Secret)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.snapshot = &configSnapshot{value: string(raw), revision: configRevision}
	return cfg, nil
}

// Save is a compare-and-swap: authorization and mutations share the same
// revision, so delayed messages and commands cannot overwrite revocations.
func saveConfig(cfg Config) error {
	configMu.Lock()
	defer configMu.Unlock()
	return saveConfigLocked(cfg)
}

func saveConfigLocked(cfg Config) error {
	if cfg.snapshot != nil {
		current, err := loadConfigLocked()
		if err != nil {
			return err
		}
		if *current.snapshot != *cfg.snapshot {
			return errConfigChanged
		}
	}
	if err := connect.DefaultFileStore().Save(section, cfg); err != nil {
		return err
	}
	configRevision++
	return nil
}

func setupInstructions() string {
	return fmt.Sprintf(`WeCom is not configured.

1. 在企业微信管理后台创建“智能机器人”，选择 API 模式和长连接。
2. 获取 Bot ID 和 Secret。
3. 在这里配置：

/connect wecom add <bot-id> <secret>

Config path: %s`, connect.DefaultFileStore().Path())
}

// prepareMessage serializes authorization and owner metadata with config edits.
// Concurrent messages must not invalidate each other's authorization snapshots.
func prepareMessage(userID, chatID, text string) (Config, bool, error) {
	configMu.Lock()
	defer configMu.Unlock()
	cfg, err := loadConfigLocked()
	if err != nil {
		return Config{}, false, err
	}
	paired := false
	if cfg.isAllowed(userID) {
		cfg.touchOwner(userID, chatID)
	} else if cfg.Valid(text, time.Now().Unix()) {
		cfg.finishPairing(userID, chatID)
		paired = true
	} else {
		return cfg, false, nil
	}
	if err := saveConfigLocked(cfg); err != nil {
		return Config{}, false, err
	}
	return cfg, paired, nil
}
