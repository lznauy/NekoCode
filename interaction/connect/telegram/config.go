package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nekocode/interaction/connect"
)

const pairingTTL = connect.DefaultPairingTTL

const section = "telegram"

// Serialize Telegram read/check/write transactions, including command writes.
// Snapshots reject stale commands instead of overwriting concurrent revocations.
var configMu sync.Mutex
var configRevision uint64

type configSnapshot struct {
	value    string
	revision uint64
}

var errConfigChanged = errors.New("telegram configuration changed; retry the command")

type Config struct {
	snapshot      *configSnapshot
	ActiveProfile string       `json:"active_profile,omitempty"`
	Profiles      []BotProfile `json:"profiles,omitempty"`

	// Legacy single-bot fields. loadConfig migrates these into Profiles and
	// saveConfig writes only the profile format.
	BotToken       string   `json:"bot_token,omitempty"`
	BotUsername    string   `json:"bot_username,omitempty"`
	AllowedUsers   []int64  `json:"allowed_users,omitempty"`
	Devices        []Device `json:"devices,omitempty"`
	UpdateOffset   int      `json:"update_offset,omitempty"`
	PairingNonce   string   `json:"pairing_nonce,omitempty"`
	PairingExpires int64    `json:"pairing_expires,omitempty"`
}

type BotProfile struct {
	Name         string  `json:"name"`
	BotToken     string  `json:"bot_token,omitempty"`
	BotUsername  string  `json:"bot_username,omitempty"`
	Owner        *Device `json:"owner,omitempty"`
	UpdateOffset int     `json:"update_offset,omitempty"`
	connect.Pairing
}

// Device is the paired telegram user; the lifecycle primitives live in the
// shared connector core (int64 user/chat IDs).
type Device = connect.Owner[int64]

func configPath() string {
	return connect.DefaultFileStore().Path()
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
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.snapshot = &configSnapshot{value: string(raw), revision: configRevision}
	return cfg, nil
}

func saveConfig(cfg Config) error {
	configMu.Lock()
	defer configMu.Unlock()
	if cfg.snapshot != nil {
		current, err := loadConfigLocked()
		if err != nil {
			return err
		}
		if *current.snapshot != *cfg.snapshot {
			return errConfigChanged
		}
	}
	return saveConfigLocked(cfg)
}

func saveConfigLocked(cfg Config) error {
	cfg.normalize() // includes clearLegacy
	if err := connect.DefaultFileStore().Save(section, cfg); err != nil {
		return err
	}
	configRevision++
	return nil
}

func (c *Config) normalize() {
	if len(c.Profiles) == 0 && strings.TrimSpace(c.BotToken) != "" {
		p := BotProfile{
			Name:         "default",
			BotToken:     strings.TrimSpace(c.BotToken),
			BotUsername:  strings.TrimSpace(c.BotUsername),
			UpdateOffset: c.UpdateOffset,
			Pairing: connect.Pairing{
				Nonce:   c.PairingNonce,
				Expires: c.PairingExpires,
			},
		}
		if len(c.Devices) > 0 {
			owner := c.Devices[0]
			p.Owner = &owner
		} else if len(c.AllowedUsers) > 0 {
			p.Owner = &Device{UserID: c.AllowedUsers[0]}
		}
		c.Profiles = append(c.Profiles, p)
	}
	for i := range c.Profiles {
		c.Profiles[i].Name = normalizeProfileName(c.Profiles[i].Name)
		if c.Profiles[i].Name == "" {
			c.Profiles[i].Name = fmt.Sprintf("profile-%d", i+1)
		}
		c.Profiles[i].BotToken = strings.TrimSpace(c.Profiles[i].BotToken)
		c.Profiles[i].BotUsername = strings.TrimPrefix(strings.TrimSpace(c.Profiles[i].BotUsername), "@")
	}
	c.ActiveProfile = normalizeProfileName(c.ActiveProfile)
	if c.ActiveProfile == "" && len(c.Profiles) > 0 {
		c.ActiveProfile = c.Profiles[0].Name
	}
	if c.ActiveProfile != "" && c.profileIndex(c.ActiveProfile) < 0 && len(c.Profiles) > 0 {
		c.ActiveProfile = c.Profiles[0].Name
	}
	c.clearLegacy()
}

func (c *Config) clearLegacy() {
	c.BotToken = ""
	c.BotUsername = ""
	c.AllowedUsers = nil
	c.Devices = nil
	c.UpdateOffset = 0
	c.PairingNonce = ""
	c.PairingExpires = 0
}

func normalizeProfileName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "@")
	name = strings.TrimSpace(name)
	return name
}

func (c Config) profileIndex(name string) int {
	name = normalizeProfileName(name)
	for i := range c.Profiles {
		if normalizeProfileName(c.Profiles[i].Name) == name {
			return i
		}
	}
	return -1
}

func (c Config) activeIndex() int {
	return c.profileIndex(c.ActiveProfile)
}

func (c Config) activeProfile() (BotProfile, bool) {
	idx := c.activeIndex()
	if idx < 0 {
		return BotProfile{}, false
	}
	return c.Profiles[idx], true
}

func (p BotProfile) pairedChatIDs() []int64 {
	if p.Owner == nil || p.Owner.ChatID == 0 {
		return nil
	}
	return []int64{p.Owner.ChatID}
}

func (p BotProfile) isAllowed(userID int64) bool {
	return p.Owner.Matches(userID)
}

func (p *BotProfile) setOwner(userID int64, username string, chatID int64) {
	connect.SetOwner(&p.Owner, userID, username, chatID, time.Now())
}

func (p *BotProfile) touchOwner(userID int64, username string, chatID int64) {
	p.Owner.Touch(userID, username, chatID, time.Now())
}

func setupInstructions() string {
	return fmt.Sprintf(`Telegram is not configured.

1. Open @BotFather in Telegram.
2. Create a bot with /newbot.
3. Paste the token here:

/connect telegram add <bot-token>

Config path: %s`, configPath())
}
