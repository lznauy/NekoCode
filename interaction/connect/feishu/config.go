package feishu

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
	section    = "feishu"
	pairingTTL = connect.DefaultPairingTTL
)

var configMu sync.Mutex
var configRevision uint64
var errConfigChanged = errors.New("configuration changed; retry the command")

type configSnapshot struct {
	value    string
	revision uint64
}

// Config is the feishu connector configuration: a single app (app_id +
// app_secret) paired with one owner (DM-only MVP, no multi-profile).
// The pairing state machine and owner lifecycle come from the shared
// connector connect.
type Config struct {
	snapshot  *configSnapshot
	AppID     string                 `json:"app_id,omitempty"`
	AppSecret string                 `json:"app_secret,omitempty"`
	Owner     *connect.Owner[string] `json:"owner,omitempty"`
	connect.Pairing
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

func (c Config) configured() bool {
	return strings.TrimSpace(c.AppID) != "" && strings.TrimSpace(c.AppSecret) != ""
}

func (c Config) isAllowed(openID string) bool {
	return c.Owner.Matches(openID)
}

// finishPairing binds the owner and clears the pairing state.
func (c *Config) finishPairing(openID, chatID string) {
	connect.SetOwner(&c.Owner, openID, "", chatID, time.Now())
	c.Clear()
}

func (c *Config) unpair() {
	c.Owner = nil
	c.Clear()
}

func (c *Config) touchOwner(openID, chatID string) {
	c.Owner.Touch(openID, "", chatID, time.Now())
}

func (c Config) pairedChatIDs() []string {
	if c.Owner == nil || c.Owner.ChatID == "" {
		return nil
	}
	return []string{c.Owner.ChatID}
}

func setupInstructions() string {
	return fmt.Sprintf(`Feishu is not configured.

1. Create an app at https://open.feishu.cn (enable the bot capability).
2. Subscribe to the im.message.receive_v1 event and enable long-connection mode.
3. Paste the credentials here:

/connect feishu add <app-id> <app-secret>

Config path: %s`, connect.DefaultFileStore().Path())
}

// prepareMessage serializes authorization and owner metadata with config edits.
// Concurrent messages must not invalidate each other's authorization snapshots.
func (c *Connector) prepareMessage(client *feishuClient, userID, chatID, text string) (Config, bool, error) {
	c.apiMu.Lock()
	defer c.apiMu.Unlock()
	if client == nil || c.api != client {
		return Config{}, false, errConfigChanged
	}
	configMu.Lock()
	defer configMu.Unlock()
	cfg, err := loadConfigLocked()
	if err != nil {
		return Config{}, false, err
	}
	if !client.matchesConfig(cfg) {
		return Config{}, false, errConfigChanged
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
