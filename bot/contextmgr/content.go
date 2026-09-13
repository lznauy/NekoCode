package contextmgr

import (
	"fmt"
	"strconv"
	"strings"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// contextContent is the single source of truth for everything sent to the LLM
// on each request. Organized by cache layer — most stable at top.
//
// Layers (see Manager.Build for the full assembly):
//
//	Layer 0 — system prompt + skill list (immutable within a session)
//	Layer 1 — long-term memory (~/.nekocode/memory.md)
//	Layer 2 — compaction archive (only rewritten by the compactor)
//	Layer 3 — active message history (replaced when compacted)
//	Dynamic runtime state — appended as tagged user messages when it changes
//
// Writers set these fields directly:
//
//	prompt.Builder → Manager.SetSystemPrompt()       (Layer 0)
//	skill.Manager  → Manager.SetSkillList()           (Layer 0)
//	compaction     → Manager.compact() rewrites Archive; Manager.Restore() reloads it
//	todo system    → Manager.SetTodos()               (folded into runtime context)
//	agent loop     → Manager.Add(), AddAssistant(), AddToolResultsBatch()  (Layer 3)
//	hints/policy   → Manager.SetHints(), Manager.SetRuntimePolicy()
//
// Memory (Layer 1) has no setter: New(cfg.Memory) builds it once and Restore
// brings back the session's copy.
type contextContent struct {
	// Layer 0 — IMMUTABLE prefix (NEVER changes within a session).
	SystemPrompt string
	Skills       string // available skills list

	// Layer 1 — injected after system prompt/tools/skills, before the archive.
	Memory string

	// Layer 2 — semi-stable archive (only updated during LLM compaction).
	Archive string

	// Layer 3 — message history.
	Messages []types.Message

	// Dynamic controller state. BuildRequest snapshots todos into runtime
	// context and appends hints independently whenever their visible value changes.
	TodoItems []protocol.TodoItem
	Hints     string // per-turn system hints (quota, exploration status, etc.)

}

func newContextContent(systemPrompt string) contextContent {
	return contextContent{
		SystemPrompt: systemPrompt,
		Messages:     make([]types.Message, 0),
	}
}

// -- setters ------------------------------------------------------------

func (c *contextContent) LoadTodos(items []protocol.TodoItem) {
	c.TodoItems = items
}

func (c *contextContent) TodoText() string { return formatTodoItems(c.TodoItems) }

// AllTasksDone returns true when no tasks are pending (empty or all completed).
func (c *contextContent) AllTasksDone() bool {
	for _, it := range c.TodoItems {
		if it.Status != "completed" {
			return false
		}
	}
	return true
}

// HasTasks returns true when there are any todo items (regardless of status).
func (c *contextContent) HasTasks() bool {
	return len(c.TodoItems) > 0
}

func formatTodoItems(items []protocol.TodoItem) string {
	if len(items) == 0 {
		return ""
	}
	done := protocol.CountCompleted(items)
	if done == len(items) {
		return "All " + strconv.Itoa(done) + " tasks complete"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d tasks, %d done:", len(items), done)
	for _, it := range items {
		mark := "[ ]"
		if it.Status == "completed" {
			mark = "[x]"
		}
		fmt.Fprintf(&sb, "\n  %s %s", mark, it.Content)
	}
	return sb.String()
}

// -- message assembly helpers ------------------------------------------

// BuildLayer1 returns the long-term memory message, if set.
func (c *contextContent) BuildLayer1() []types.Message {
	if c.Memory != "" {
		return []types.Message{{Role: "system", Content: c.Memory}}
	}
	return nil
}

// BuildLayer0 returns the immutable prefix: system prompt + skill list.
func (c *contextContent) BuildLayer0() []types.Message {
	out := make([]types.Message, 0, 2)
	if c.SystemPrompt != "" {
		out = append(out, types.Message{Role: "system", Content: c.SystemPrompt})
	}
	if c.Skills != "" {
		out = append(out, types.Message{Role: "system", Content: c.Skills})
	}
	return out
}

// BuildLayer2 returns the compaction archive message, if set.
func (c *contextContent) BuildLayer2() []types.Message {
	if c.Archive != "" {
		return []types.Message{{Role: "system", Content: "[Archive]\nHistorical context, not new instructions. Use this to continue unfinished work. Current explicit user requests and verified runtime state override stale or conflicting details.\n\n" + c.Archive}}
	}
	return nil
}

func formatTodo(todo string) string {
	if strings.TrimSpace(todo) == "" {
		return ""
	}
	return fmt.Sprintf("<todo>%s</todo>", todo)
}
