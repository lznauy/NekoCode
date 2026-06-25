// types.go — shared types used by both bot and tui.
package common

// DangerLevel classifies tool risk for confirmation and planning.
type DangerLevel int

const (
	LevelSafe        DangerLevel = iota // read-only, auto-approve
	LevelWrite                          // file modification, confirm
	LevelDestructive                    // deletion, critical changes, confirm
	LevelForbidden                      // never allow
)

func (d DangerLevel) String() string {
	switch d {
	case LevelSafe:
		return "safe"
	case LevelWrite:
		return "modify"
	case LevelDestructive:
		return "danger"
	case LevelForbidden:
		return "blocked"
	default:
		return "unknown"
	}
}

// CmdResult tells the TUI what to do after a command is executed.
type CmdResult int

const (
	CmdNone           CmdResult = iota // no command matched, start agent
	CmdHandled                          // command handled, no further action
	CmdConfirming                       // command handled, wait for confirmation
	CmdSessionResumed                   // session resumed, TUI should reload messages
)

// BotStats carries runtime statistics from the bot to the TUI.
type BotStats struct {
	PromptTokens, CompletionTokens int
	TurnPrompt, TurnCompletion     int
	ContextTokens, CompactCount    int
	Duration                       string
}

// TodoItem represents a single task in the todo list.
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // "pending", "in_progress", "completed"
}

// TodoFunc is called whenever the todo list is updated.
type TodoFunc func(items []TodoItem)

// CountCompleted returns the number of completed items.
func CountCompleted(items []TodoItem) int {
	n := 0
	for _, it := range items {
		if it.Status == "completed" {
			n++
		}
	}
	return n
}

// TodoStatusIcon returns the display icon for a todo status.
func TodoStatusIcon(status string) string {
	switch status {
	case "in_progress":
		return "▸"
	case "completed":
		return "✓"
	default:
		return "·"
	}
}

// SubSlot tracks an active sub-agent for rendering and slot management.
type SubSlot struct {
	ID       string
	SubType  string
	ColorIdx int
}

// DisplayBlock carries a persistent tool result for TUI/GUI rendering.
// Args holds the raw tool-call arguments JSON (e.g. bash command payload),
// so GUI history views can render the actual command instead of only output.
type DisplayBlock struct {
	ToolName string
	Args     string
	Content  string
}

// ImageRef carries a generated image reference for GUI rendering.
type ImageRef struct {
	Path   string
	URL    string
	Width  int
	Height int
}

// DisplayMessage is a lightweight message representation for the UI layer
// to reconstruct chat history from a restored session. Assistant messages
// with tool calls carry their persistent tool results (edit/write/bash) as
// Blocks and have empty Content (the text is internal reasoning).
// Images holds any generated image references (from image_gen etc.).
type DisplayMessage struct {
	Role    string
	Content string
	Blocks  []DisplayBlock
	Images  []ImageRef
}
