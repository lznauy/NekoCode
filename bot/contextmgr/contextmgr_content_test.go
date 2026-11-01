package contextmgr

import (
	"nekocode/protocol"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
)

func TestNewContextContent(t *testing.T) {
	c := newContextContent("you are helpful")
	if c.SystemPrompt != "you are helpful" {
		t.Errorf("SystemPrompt = %q", c.SystemPrompt)
	}
	if len(c.Messages) != 0 {
		t.Error("Messages should be empty")
	}
}

func TestLoadTodos(t *testing.T) {
	c := newContextContent("")
	c.LoadTodos([]protocol.TodoItem{
		{Content: "task 1", Status: "pending"},
		{Content: "task 2", Status: "completed"},
	})
	if c.TodoText() == "" {
		t.Error("Todo should not be empty")
	}
	if !strings.Contains(c.TodoText(), "task 1") {
		t.Errorf("Todo missing task: %s", c.TodoText())
	}
}

func TestAllTasksDone(t *testing.T) {
	c := newContextContent("")
	if !c.AllTasksDone() {
		t.Error("empty list should be 'done'")
	}
	c.LoadTodos([]protocol.TodoItem{
		{Content: "a", Status: "completed"},
	})
	if !c.AllTasksDone() {
		t.Error("all completed should be 'done'")
	}
	c.LoadTodos([]protocol.TodoItem{
		{Content: "a", Status: "pending"},
	})
	if c.AllTasksDone() {
		t.Error("pending tasks should not be 'done'")
	}
}

func TestBuildLayer0(t *testing.T) {
	c := newContextContent("system")
	c.Skills = "skill list"
	msgs := c.BuildLayer0()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "system" {
		t.Errorf("msg0 = %q", msgs[0].Content)
	}
	if msgs[1].Content != "skill list" {
		t.Errorf("msg1 = %q", msgs[1].Content)
	}
}

func TestBuildLayer0_Empty(t *testing.T) {
	c := newContextContent("")
	msgs := c.BuildLayer0()
	if len(msgs) != 1 || msgs[0].Role != "system" || msgs[0].Content != historicalDataContract {
		t.Error("empty system/skills should retain the static historical data contract")
	}
}

func TestBuildMemoryData(t *testing.T) {
	c := newContextContent("")
	c.Memory = "memory content"
	msgs := c.BuildData()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 msg, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Source != types.MessageSourceContextData || !strings.Contains(msgs[0].Content, "memory content") {
		t.Error("memory message incorrect")
	}
}

func TestBuildDataEmpty(t *testing.T) {
	c := newContextContent("")
	msgs := c.BuildData()
	if len(msgs) != 0 {
		t.Error("empty memory should produce no messages")
	}
}

func TestBuildArchiveData(t *testing.T) {
	c := newContextContent("")
	c.Archive = "archived summary"
	msgs := c.BuildData()
	if len(msgs) != 1 {
		t.Fatal("expected 1 message")
	}
	if msgs[0].Role != "user" || !strings.Contains(msgs[0].Content, `"kind":"archive"`) {
		t.Errorf("archive message: %s", msgs[0].Content)
	}
}

func TestRenderRuntimeContext(t *testing.T) {
	c := newContextContent("")
	c.LoadTodos([]protocol.TodoItem{{Content: "x", Status: "pending"}})
	got := renderRuntimeContext("<environment>cwd</environment>", c.TodoText(), "")
	for _, want := range []string{"<runtime_context mode=\"replace\">", "supersedes", "<environment>", "<todo>", `runtime_policy_state state="inactive"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("runtime context missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "hint") {
		t.Fatalf("runtime state must not contain hints: %s", got)
	}
}

func TestRenderRuntimeContextEmpty(t *testing.T) {
	got := renderRuntimeContext("", "", "")
	for _, want := range []string{`environment_state state="unavailable"`, `todo_state state="empty"`, `runtime_policy_state state="inactive"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("empty runtime snapshot missing %q: %s", want, got)
		}
	}
}

func TestFormatTodoItems_AllDone(t *testing.T) {
	c := newContextContent("")
	c.LoadTodos([]protocol.TodoItem{
		{Content: "a", Status: "completed"},
		{Content: "b", Status: "completed"},
	})
	if !strings.Contains(c.TodoText(), "All 2 tasks complete") {
		t.Errorf("all done: %s", c.TodoText())
	}
}

func TestFormatTodoItems_Mixed(t *testing.T) {
	c := newContextContent("")
	c.LoadTodos([]protocol.TodoItem{
		{Content: "a", Status: "completed"},
		{Content: "b", Status: "pending"},
	})
	if !strings.Contains(c.TodoText(), "[x]") || !strings.Contains(c.TodoText(), "[ ]") {
		t.Errorf("mixed: %s", c.TodoText())
	}
}

func TestAddMessage(t *testing.T) {
	c := newContextContent("")
	c.Messages = append(c.Messages, types.Message{Role: "user", Content: "hi"})
	if len(c.Messages) != 1 {
		t.Error("message not added")
	}
}

func TestFormatTodo(t *testing.T) {
	s := formatTodo("do stuff")
	if s != "<todo>do stuff</todo>" {
		t.Errorf("formatTodo = %q", s)
	}
}
