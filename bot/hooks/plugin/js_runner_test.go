package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPluginJSLoadsPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hook.js"), []byte(`
		hook = function(event) {
			return { require_tool: { tool: "verify", reason: event.error ? "failed" : "ok" } };
		}
	`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := runPluginJS(root, Event{Error: true}, hookAction{Type: "js", Path: "hook.js"})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.RequireTool == nil {
		t.Fatalf("result = %+v, want require tool", got)
	}
	if got.RequireTool.Tool != "verify" || got.RequireTool.Reason != "failed" {
		t.Fatalf("require tool = %+v", got.RequireTool)
	}
}

func TestRunPluginJSRejectsPathTraversal(t *testing.T) {
	_, err := runPluginJS(t.TempDir(), Event{}, hookAction{Type: "js", Path: "../hook.js"})
	if err == nil {
		t.Fatal("path traversal should fail")
	}
}

func TestRunPluginJSConsoleLogBecomesDiagnosticHintWithoutResult(t *testing.T) {
	got, err := runPluginJS(t.TempDir(), Event{}, hookAction{
		Type: "js",
		Code: `console.log("state", 3)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Hint == nil {
		t.Fatalf("result = %+v, want console hint", got)
	}
	if got.Hint.Type != "plugin_console" || !strings.Contains(got.Hint.Content, "[log] state 3") {
		t.Fatalf("hint = %+v", got.Hint)
	}
}

func TestRunPluginJSReadFileIsLimitedToPluginRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"mode":"strict"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := runPluginJS(root, Event{}, hookAction{
		Type: "js",
		Code: `hook = function() {
			var cfg = JSON.parse(readFile("config.json"));
			return { hint: { type: "config", content: cfg.mode } };
		}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Hint == nil || got.Hint.Content != "strict" {
		t.Fatalf("result = %+v, want config hint", got)
	}

	_, err = runPluginJS(root, Event{}, hookAction{
		Type: "js",
		Code: `readFile("../outside.txt")`,
	})
	if err == nil {
		t.Fatal("readFile path traversal should fail")
	}
}
