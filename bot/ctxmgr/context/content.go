package context

import (
	"nekocode/common"
	"strconv"
	"strings"

	"nekocode/bot/ctxmgr/token"
	"nekocode/llm"
)

// Content is the single source of truth for everything sent to the LLM
// on each request. Organized by cache layer — most stable at top.
//
// External setters set fields directly:
//	prompt.Builder → Manager.SetSystemPrompt()
//	skill.Registry  → Manager.SetSkillList()
//	summarizer      → Manager.SetArchive()
//	todo system     → Manager.SetTodos()
//	agent loop      → AddMessage(), AddToolResult()
type Content struct {
	// Layer 0 — IMMUTABLE prefix (NEVER changes within a session).
	SystemPrompt string
	Skills       string // available skills list

	// Layer 0 — injected after system prompt/tools/skills, before message history.
	Memory string

	// Layer 0.5 — semi-stable archive (only updated during LLM compaction).
	Archive string

	// Layer 1 — message history.
	Messages       []llm.Message
	CompactBoundary int

	// Layer 2 — volatile suffix. ALL variable content goes HERE, after history.
	Constraints string // <critical-constraints>
	KeyFacts    string // <key-facts> (may grow after summarization)
	Todo        string
	TodoItems   []common.TodoItem // structured copy, kept in sync with Todo
	Hints       string           // per-turn system hints (quota, exploration status, etc.)

}

func New(systemPrompt string) Content {
	return Content{
		SystemPrompt: systemPrompt,
		Messages:     make([]llm.Message, 0),
	}
}

// -- setters ------------------------------------------------------------

func (c *Content) LoadTodos(items []common.TodoItem) {
	c.TodoItems = items
	c.Todo = formatTodoItems(items)
}

// AllTasksDone returns true when no tasks are pending (empty or all completed).
func (c *Content) AllTasksDone() bool {
	for _, it := range c.TodoItems {
		if it.Status != "completed" {
			return false
		}
	}
	return true
}

func formatTodoItems(items []common.TodoItem) string {
	if len(items) == 0 {
		return ""
	}
	done := 0
	for _, it := range items {
		if it.Status == "completed" {
			done++
		}
	}
	if done == len(items) {
		return "All " + strconv.Itoa(done) + " tasks complete"
	}
	var sb strings.Builder
	sb.WriteString(strconv.Itoa(len(items)) + " tasks, " + strconv.Itoa(done) + " done:")
	for _, it := range items {
		mark := "[ ]"
		if it.Status == "completed" {
			mark = "[x]"
		}
		sb.WriteString("\n  " + mark + " " + it.Content)
	}
	return sb.String()
}


// -- message assembly helpers ------------------------------------------

// BuildLayer0Mem returns Memory if set. Injected after system prompt/skills.
func (c *Content) BuildLayer0Mem() []llm.Message {
	if c.Memory != "" {
		return []llm.Message{{Role: "system", Content: c.Memory}}
	}
	return nil
}

func (c *Content) BuildLayer0() []llm.Message {
	out := make([]llm.Message, 0, 2)
	if c.SystemPrompt != "" {
		out = append(out, llm.Message{Role: "system", Content: c.SystemPrompt})
	}
	if c.Skills != "" {
		out = append(out, llm.Message{Role: "system", Content: c.Skills})
	}
	return out
}

// BuildLayer05 returns the Archive message (Layer 0.5), if set.
func (c *Content) BuildLayer05() []llm.Message {
	if c.Archive != "" {
		return []llm.Message{{Role: "system", Content: "[Archive]\n" + c.Archive}}
	}
	return nil
}

func (c *Content) BuildLayer2() []llm.Message {
	var out []llm.Message
	if c.Constraints != "" {
		out = append(out, llm.Message{Role: "system", Content: c.Constraints})
	}
	if c.KeyFacts != "" {
		out = append(out, llm.Message{Role: "system", Content: c.KeyFacts})
	}
	if c.Todo != "" {
		out = append(out, llm.Message{Role: "system", Content: FormatTodo(c.Todo)})
	}
	if c.Hints != "" {
		out = append(out, llm.Message{Role: "system", Content: c.Hints})
	}
	return out
}

// DynamicSuffixTokens estimates tokens consumed by the Layer 2 suffix.
func (c *Content) DynamicSuffixTokens() int {
	n := 0
	if c.Todo != "" {
		n += token.EstimateString(c.Todo) + 20
	}
	if c.Hints != "" {
		n += token.EstimateString(c.Hints) + 20
	}
	return n
}
