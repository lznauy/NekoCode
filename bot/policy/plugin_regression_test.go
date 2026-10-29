package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginEntriesWithSameMatcherAllRun(t *testing.T) {
	root := t.TempDir()
	config := `{"PreToolUse":[{"matcher":"write","hooks":[{"type":"js","code":"({hint:{content:'first'}})"}]},{"matcher":"write","hooks":[{"type":"js","code":"({hint:{content:'second'}})"}]}]}`
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	hooks, err := LoadPluginHooks(root, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	p := New()
	for _, h := range hooks {
		p.Register(h)
	}
	got := p.BeforeTool(ToolRequest{Name: "write"})
	if len(got) != 2 || got[0].Hint.Content != "first" || got[1].Hint.Content != "second" {
		t.Fatalf("results = %+v; want both entries in order", got)
	}
}

func TestPluginJSReadsPreviousStateAndEvent(t *testing.T) {
	root := t.TempDir()
	config := `{"PreToolUse":[{"matcher":"write","hooks":[{"type":"js","code":"hook = function(e) { var n = (e.state.ints.count || 0) + 1; return {state_patch:{ints:{count:n}},hint:{content:e.args.path + ':' + n + ':' + e.pluginRoot}}; }"}]}]}`
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	hooks, err := LoadPluginHooks(root, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	p := New()
	for _, h := range hooks {
		p.Register(h)
	}
	for _, want := range []string{"main.go:1:" + root, "main.go:2:" + root} {
		got := p.BeforeTool(ToolRequest{Name: "write", Args: map[string]any{"path": "main.go"}})
		if len(got) != 1 || got[0].Hint == nil || got[0].Hint.Content != want {
			t.Fatalf("results = %+v; want %q", got, want)
		}
	}
	p.ResetRun()
	got := p.BeforeTool(ToolRequest{Name: "write", Args: map[string]any{"path": "main.go"}})
	if len(got) != 1 || got[0].Hint == nil || got[0].Hint.Content != "main.go:1:"+root {
		t.Fatal("state did not reset")
	}
}

func TestPluginExposesModelAndBatchEvents(t *testing.T) {
	root := t.TempDir()
	config := `{"PreModel":[{"hooks":[{"type":"js","code":"({hint:{content:'model'}})"}]}],"PostToolBatch":[{"hooks":[{"type":"js","code":"({hint:{content:'batch'}})"}]}]}`
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	hooks, err := LoadPluginHooks(root, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	p := New()
	for _, h := range hooks {
		p.Register(h)
	}
	if got := p.BeforeModel(); len(got) != 1 || got[0].Hint.Content != "model" {
		t.Fatalf("model results: %+v", got)
	}
	if got := p.RecordTools(nil); len(got) != 1 || got[0].Hint.Content != "batch" {
		t.Fatalf("batch results: %+v", got)
	}
}

func TestPluginLifecyclePreservesActorIdentity(t *testing.T) {
	root := t.TempDir()
	config := `{"SubagentStart":[{"hooks":[{"type":"js","code":"hook = function(e) { return {hint:{content:e.actor_id}} }"}]}]}`
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	hooks, err := LoadPluginHooks(root, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	p := New()
	p.Register(hooks[0])
	out := Resolve(p.hooks.evaluate(SubagentStart, Facts{ActorID: "child-123"}))
	if len(out.Hints) != 1 || out.Hints[0].Content != "child-123" {
		t.Fatalf("outcome=%+v", out)
	}
}

func TestPermissionPluginObserversUseV1AndCannotDecide(t *testing.T) {
	root := t.TempDir()
	config := `{"version":1,"PermissionRequest":[{"matcher":"shell","mode":"observer","hooks":[{"type":"js","code":"({state_patch:{strings:{seen:event.args.call_id + ':' + event.actor_id}}})"}]}],"PermissionDenied":[{"matcher":"shell","mode":"observer","hooks":[{"type":"js","code":"({decision:{action:'continue'},state_patch:{strings:{source:event.args.source}}})"}]}]}`
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	hooks, err := LoadPluginHooks(root, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	gov := New()
	for _, hook := range hooks {
		if err := gov.Register(hook); err != nil {
			t.Fatal(err)
		}
	}
	gov.Observe(t.Context(), PermissionRequest, Facts{ActorID: "child", Tool: ToolFacts{Name: "shell", Args: map[string]any{"call_id": "call"}}})
	gov.Observe(t.Context(), PermissionDenied, Facts{ActorID: "child", Tool: ToolFacts{Name: "shell", Error: true, Args: map[string]any{"source": "rule"}}})
	// Inspect committed observer state, rather than unused hint output.
	requestMemory := gov.hooks.state[hooks[0].Name]
	deniedMemory := gov.hooks.state[hooks[1].Name]
	if requestMemory.strings["seen"] != "call:child" || deniedMemory.strings["source"] != "rule" {
		t.Fatalf("request=%+v denied=%+v", requestMemory, deniedMemory)
	}
}
