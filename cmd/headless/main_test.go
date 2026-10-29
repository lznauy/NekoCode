package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/bot/config"
)

func TestCLIProcessHelper(t *testing.T) {
	if os.Getenv("NEKOCODE_ONCE_CLI_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"nekocode-headless"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	os.Exit(99)
}

func TestCLIOnceAndResume(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workspace := filepath.Join(root, "project")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		mu.Lock()
		requests = append(requests, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []any{map[string]any{"index": 0, "delta": map[string]any{"content": `{"ok":true}`}}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}} {
			data, _ := json.Marshal(map[string]any{"id": "once", "object": "chat.completion.chunk", "choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	t.Setenv("HOME", home)
	if err := config.Save(config.Config{Active: "openai/test", Providers: []config.ProviderConfig{{Name: "openai", Protocol: "openai", BaseURL: server.URL, APIKey: "test-key", Models: []config.ModelDefinition{{Name: "test", Model: "gpt-4o"}}}}}); err != nil {
		t.Fatal(err)
	}
	run := func(input string, args ...string) (string, string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIProcessHelper$", "--"}, args...)...)
		command.Env = append(os.Environ(), "NEKOCODE_ONCE_CLI_HELPER=1")
		command.Dir = workspace
		command.Stdin = strings.NewReader(input)
		var out, diagnostics bytes.Buffer
		command.Stdout = &out
		command.Stderr = &diagnostics
		err := command.Run()
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		return out.String(), diagnostics.String(), code
	}
	out, diagnostics, code := run("", "exec", "first task", "--output-format=json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, diagnostics, out)
	}
	var result struct {
		SessionID string `json:"session_id"`
		Result    string `json:"result"`
		IsError   bool   `json:"is_error"`
	}
	if json.Unmarshal([]byte(out), &result) != nil || result.SessionID == "" || result.IsError || result.Result != `{"ok":true}` {
		t.Fatalf("bad CLI JSON: %s", out)
	}
	out, diagnostics, code = run("second task", "exec", "--resume", result.SessionID, "-")
	if code != 0 || out != "{\"ok\":true}\n" {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, diagnostics, out)
	}
	mu.Lock()
	history := strings.Join(requests, "\n")
	mu.Unlock()
	if !strings.Contains(history, "first task") || !strings.Contains(history, "second task") {
		t.Fatalf("resume lost history: %s", history)
	}
	schema := filepath.Join(root, "schema.json")
	if err := os.WriteFile(schema, []byte(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostics, code = run("", "-p", "schema task", "--output-format=json", "--output-schema", schema)
	if code != 0 || !strings.Contains(out, `"structured_output":{"ok":true}`) {
		t.Fatalf("schema exit=%d stderr=%s stdout=%s", code, diagnostics, out)
	}
	if err := os.WriteFile(schema, []byte(`{"type":"object","required":["missing"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostics, code = run("", "exec", "invalid result", "--output-schema", schema)
	if code != 1 || out != "" || !strings.Contains(diagnostics, "does not match schema") {
		t.Fatalf("mismatch exit=%d stderr=%s stdout=%s", code, diagnostics, out)
	}
	out, diagnostics, code = run("", "exec", "--unknown")
	if code != 2 || out != "" {
		t.Fatalf("arguments exit=%d stderr=%s stdout=%s", code, diagnostics, out)
	}
	out, diagnostics, code = run("", "exec", "-")
	if code != 2 || out != "" {
		t.Fatalf("empty stdin exit=%d stderr=%s stdout=%s", code, diagnostics, out)
	}
}
