package connect

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekocode/protocol"
	controlruntime "nekocode/runtime"
)

const (
	commandMenuTTL  = 5 * time.Minute
	maxCommandMenus = 64
	menuPageSize    = 8
)

// MenuChoice is one transport-ready command choice. Token is deliberately
// short and opaque so chat callbacks never need to carry a command, path, or
// connector-specific payload limit.
type MenuChoice struct {
	Token       string
	Label       string
	Description string
}

type MenuPrompt struct {
	Title   string
	Empty   string
	Choices []MenuChoice
}

// MenuResult describes the one action a connector should take after command
// menu handling. At most one of Prompt, Command, and Message is populated.
type MenuResult struct {
	Prompt  *MenuPrompt
	Command string
	Message string
	Handled bool
}

type pendingCommandMenu struct {
	scope   string
	title   string
	empty   string
	created time.Time
	expires time.Time
	items   []protocol.CommandMenuItem
}

type menuPosition struct {
	id   string
	page int
}

// CommandMenus is the bounded, shared callback state used by stateless chat
// transports. Runtime menu discovery remains stateless; only presentation
// tokens and numeric replies live here.
type CommandMenus struct {
	mu      sync.Mutex
	next    uint64
	pending map[string]pendingCommandMenu
	latest  map[string]menuPosition
}

func NewCommandMenus() *CommandMenus {
	return &CommandMenus{pending: make(map[string]pendingCommandMenu), latest: make(map[string]menuPosition)}
}

// Clear drops all pending menu callbacks when a connector identity changes.
func (m *CommandMenus) Clear() {
	m.mu.Lock()
	clear(m.pending)
	clear(m.latest)
	m.mu.Unlock()
}

// HandleText opens a menu for a command or resolves a numeric reply to the
// most recent menu in this conversation. Non-menu text is left untouched.
func (m *CommandMenus) HandleText(ctx context.Context, rt controlruntime.ConnectorRuntime, scope, text string) MenuResult {
	text = strings.TrimSpace(text)
	if text == "" || rt == nil {
		return MenuResult{}
	}
	if index, err := strconv.Atoi(text); err == nil {
		if token, pending := m.numericToken(scope, index); token != "" {
			return m.Select(ctx, rt, scope, token)
		} else if pending {
			return MenuResult{Handled: true, Message: "请输入菜单中的有效序号。"}
		}
	}
	query := text
	if text == "/help" {
		query = "/"
	}
	menu, ok := rt.CommandMenu(ctx, query)
	if !ok {
		return MenuResult{}
	}
	return MenuResult{Prompt: m.open(scope, menu), Handled: true}
}

// Select resolves one callback token and either opens the next menu or returns
// the complete command that should be submitted through the normal run path.
func (m *CommandMenus) Select(ctx context.Context, rt controlruntime.ConnectorRuntime, scope, token string) MenuResult {
	if strings.HasPrefix(token, "cmdp:") {
		prompt, ok := m.page(scope, token)
		if !ok {
			return MenuResult{Handled: true, Message: "菜单已失效，请重新打开命令。"}
		}
		return MenuResult{Handled: true, Prompt: prompt}
	}
	item, ok := m.resolve(scope, token)
	if !ok {
		return MenuResult{Handled: true, Message: "菜单已失效，请重新打开命令。"}
	}
	if item.Submit {
		return MenuResult{Handled: true, Command: item.Value}
	}
	menu, ok := rt.CommandMenu(ctx, item.Value)
	if !ok {
		return MenuResult{Handled: true, Message: "请发送 " + item.Value + " 并补充所需参数。"}
	}
	return MenuResult{Handled: true, Prompt: m.open(scope, menu)}
}

func (m *CommandMenus) open(scope string, menu protocol.CommandMenu) *MenuPrompt {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	m.next++
	id := strconv.FormatUint(m.next, 36)
	items := append([]protocol.CommandMenuItem(nil), menu.Items...)
	m.pending[id] = pendingCommandMenu{
		scope: scope, title: menu.Title, empty: menu.Empty,
		created: now, expires: now.Add(commandMenuTTL), items: items,
	}
	m.latest[scope] = menuPosition{id: id}
	return m.promptLocked(id, 0)
}

func (m *CommandMenus) promptLocked(id string, page int) *MenuPrompt {
	menu := m.pending[id]
	pages := max(1, (len(menu.items)+menuPageSize-1)/menuPageSize)
	page = min(max(page, 0), pages-1)
	start := page * menuPageSize
	end := min(start+menuPageSize, len(menu.items))
	choices := make([]MenuChoice, 0, end-start+2)
	for i := start; i < end; i++ {
		item := menu.items[i]
		label := item.Label
		if label == "" {
			label = item.Value
		}
		choices = append(choices, MenuChoice{
			Token: "cmd:" + id + ":" + strconv.Itoa(i), Label: label, Description: item.Description,
		})
	}
	if page > 0 {
		choices = append(choices, MenuChoice{Token: "cmdp:" + id + ":" + strconv.Itoa(page-1), Label: "← Previous"})
	}
	if page+1 < pages {
		choices = append(choices, MenuChoice{Token: "cmdp:" + id + ":" + strconv.Itoa(page+1), Label: "Next →"})
	}
	title := menu.title
	if strings.TrimSpace(title) == "" {
		title = "Commands"
	}
	if pages > 1 {
		title = fmt.Sprintf("%s · %d/%d", title, page+1, pages)
	}
	return &MenuPrompt{Title: title, Empty: menu.empty, Choices: choices}
}

func (m *CommandMenus) numericToken(scope string, index int) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	position, ok := m.latest[scope]
	if !ok {
		return "", false
	}
	if index < 1 {
		return "", true
	}
	prompt := m.promptLocked(position.id, position.page)
	if index > len(prompt.Choices) {
		return "", true
	}
	return prompt.Choices[index-1].Token, true
}

func (m *CommandMenus) page(scope, token string) (*MenuPrompt, bool) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] != "cmdp" {
		return nil, false
	}
	page, err := strconv.Atoi(parts[2])
	if err != nil || page < 0 {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	menu, ok := m.pending[parts[1]]
	if !ok || menu.scope != scope || page*menuPageSize >= max(1, len(menu.items)) {
		return nil, false
	}
	m.latest[scope] = menuPosition{id: parts[1], page: page}
	return m.promptLocked(parts[1], page), true
}

func (m *CommandMenus) resolve(scope, token string) (protocol.CommandMenuItem, bool) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] != "cmd" {
		return protocol.CommandMenuItem{}, false
	}
	index, err := strconv.Atoi(parts[2])
	if err != nil {
		return protocol.CommandMenuItem{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	menu, ok := m.pending[parts[1]]
	if !ok || menu.scope != scope || index < 0 || index >= len(menu.items) {
		return protocol.CommandMenuItem{}, false
	}
	item := menu.items[index]
	if item.Submit {
		delete(m.pending, parts[1])
		if m.latest[scope].id == parts[1] {
			delete(m.latest, scope)
		}
	}
	return item, true
}

func (m *CommandMenus) cleanupLocked(now time.Time) {
	for id, menu := range m.pending {
		if !now.Before(menu.expires) {
			delete(m.pending, id)
			if m.latest[menu.scope].id == id {
				delete(m.latest, menu.scope)
			}
		}
	}
	if len(m.pending) < maxCommandMenus {
		return
	}
	ids := make([]string, 0, len(m.pending))
	for id := range m.pending {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return m.pending[ids[i]].created.Before(m.pending[ids[j]].created) })
	for len(m.pending) >= maxCommandMenus && len(ids) > 0 {
		id := ids[0]
		ids = ids[1:]
		menu := m.pending[id]
		delete(m.pending, id)
		if m.latest[menu.scope].id == id {
			delete(m.latest, menu.scope)
		}
	}
}

type connectorMenuAction struct {
	name        string
	description string
	input       bool
	profiles    bool
	choices     []string
}

var connectorMenuTitles = map[string]string{
	"telegram": "Telegram",
	"feishu":   "Feishu",
	"wecom":    "WeCom",
	"qqbot":    "QQBot",
}

var connectorMenuActions = map[string][]connectorMenuAction{
	"telegram": {
		{name: "connect", description: "Connect using the active profile"},
		{name: "add", description: "Add a bot: [name] <bot-token>", input: true},
		{name: "token", description: "Add a bot: [name] <bot-token>", input: true},
		{name: "profiles", description: "List bot profiles"},
		{name: "list", description: "List bot profiles"},
		{name: "use", description: "Switch active profile", profiles: true},
		{name: "pair", description: "Pair a profile (active by default)", profiles: true},
		{name: "unpair", description: "Unpair a profile (active by default)", profiles: true},
		{name: "remove", description: "Remove a profile", profiles: true},
		{name: "delete", description: "Remove a profile", profiles: true},
		{name: "reset", description: "Reset Telegram configuration"},
		{name: "status", description: "Show connector status"},
		{name: "disconnect", description: "Stop the connector"},
		{name: "stop", description: "Stop the connector"},
	},
	"feishu": {
		{name: "connect", description: "Connect using saved configuration"},
		{name: "add", description: "Save app credentials: <app-id> <app-secret>", input: true},
		{name: "pair", description: "Pair your Feishu account"},
		{name: "unpair", description: "Unpair your Feishu account"},
		{name: "reset", description: "Reset Feishu configuration"},
		{name: "status", description: "Show connector status"},
		{name: "disconnect", description: "Stop the connector"},
		{name: "stop", description: "Stop the connector"},
	},
	"wecom": {
		{name: "connect", description: "Connect using saved configuration"},
		{name: "add", description: "Save bot credentials: <bot-id> <secret>", input: true},
		{name: "pair", description: "Pair your WeCom account"},
		{name: "unpair", description: "Unpair your WeCom account"},
		{name: "reset", description: "Reset WeCom configuration"},
		{name: "start", description: "Start the connector"},
		{name: "status", description: "Show connector status"},
		{name: "disconnect", description: "Stop the connector"},
		{name: "stop", description: "Stop the connector"},
	},
	"qqbot": {
		{name: "add", description: "Save app credentials: <appid> <appsecret>", input: true},
		{name: "sandbox", description: "Select sandbox or production", choices: []string{"on", "off"}},
		{name: "start", description: "Start the connector"},
		{name: "stop", description: "Stop the connector"},
		{name: "disconnect", description: "Stop the connector"},
		{name: "status", description: "Show connector status"},
	},
}

// ConnectorCommandMenu exposes only finite connector actions. Actions that
// need credentials stay as input prefixes and never include configured values.
func ConnectorCommandMenu(platform string, args []string, profiles []string) (protocol.CommandMenu, bool) {
	platform = strings.ToLower(platform)
	actions, ok := connectorMenuActions[platform]
	if !ok || len(args) > 1 {
		return protocol.CommandMenu{}, false
	}
	prefix := "/connect " + platform
	if len(args) == 0 {
		items := make([]protocol.CommandMenuItem, 0, len(actions))
		for _, action := range actions {
			items = append(items, protocol.CommandMenuItem{
				Value: prefix + " " + action.name, Label: action.name,
				Description: action.description,
				Submit:      !action.input && !action.profiles && len(action.choices) == 0,
			})
		}
		return protocol.CommandMenu{Title: connectorMenuTitles[platform] + " actions", Items: items}, true
	}
	name := strings.ToLower(args[0])
	for _, action := range actions {
		if action.name != name {
			continue
		}
		values := action.choices
		if action.profiles {
			values = append([]string(nil), profiles...)
			sort.Strings(values)
		}
		if len(values) == 0 && !action.profiles {
			return protocol.CommandMenu{}, false
		}
		items := make([]protocol.CommandMenuItem, 0, len(values))
		for _, value := range values {
			items = append(items, protocol.CommandMenuItem{
				Value: prefix + " " + name + " " + value,
				Label: value, Submit: true,
			})
		}
		return protocol.CommandMenu{
			Title: prefix + " " + name, Empty: "No profiles configured", Items: items,
		}, true
	}
	return protocol.CommandMenu{}, false
}

// FormatMenu is the universal text fallback for transports without buttons.
func FormatMenu(prompt *MenuPrompt) string {
	if prompt == nil {
		return ""
	}
	title := strings.TrimSpace(prompt.Title)
	if title == "" {
		title = "Commands"
	}
	if len(prompt.Choices) == 0 {
		empty := strings.TrimSpace(prompt.Empty)
		if empty == "" {
			empty = "No choices available"
		}
		return title + "\n\n" + empty
	}
	var b strings.Builder
	b.WriteString(title)
	for i, choice := range prompt.Choices {
		fmt.Fprintf(&b, "\n\n%d. %s", i+1, choice.Label)
		if choice.Description != "" {
			b.WriteString(" — ")
			b.WriteString(choice.Description)
		}
	}
	b.WriteString("\n\n回复序号进行选择。")
	return b.String()
}
