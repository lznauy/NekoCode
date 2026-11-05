package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/tool/builtin/capability"
	"nekocode/bot/extension/tool/builtin/deliver"
	toolcore "nekocode/bot/extension/tool/runtime/core"
)

func TestExtraToolsPreserveExtensionRegistryAcrossRebuilds(t *testing.T) {
	t.Chdir(t.TempDir())
	b := newPersistTestBot(t)
	original := b.toolbox.Registry
	before := original.Descriptors()
	b.RegisterExtraTools(deliver.NewTool(nil))
	check := func(t *testing.T) {
		t.Helper()
		for _, descriptor := range before {
			if _, err := b.toolbox.Registry.Lookup(descriptor.Name); err != nil {
				t.Fatalf("tool %q lost: %v", descriptor.Name, err)
			}
		}
		entry, err := b.toolbox.Registry.Lookup(capability.ToolName)
		if err != nil || entry.ResolveTarget == nil {
			t.Fatalf("capability resolver lost: %+v %v", entry, err)
		}
		b.ag.Executor().SetFullAccess(true)
		results := b.ag.Executor().ExecuteBatch(context.Background(), []toolcore.ToolCallItem{
			{ID: "list", Name: "capability", Args: map[string]any{"action": "list"}},
			{ID: "deliver", Name: "deliver_file", Args: map[string]any{}},
		})
		if results[0].Error != "" || results[0].Output != "No MCP servers configured." {
			t.Fatalf("agent cannot discover MCP: %+v", results[0])
		}
		if !strings.Contains(results[1].Error, "path is required") {
			t.Fatalf("injected tool did not reach agent executor: %+v", results[1])
		}
	}
	t.Run("injection", check)
	if b.toolbox.Registry != original {
		t.Fatal("injection replaced shared registry")
	}
	b.RegisterExtraTools()
	if b.toolbox.Registry != original {
		t.Fatal("empty injection replaced shared registry")
	}
	if err := b.SwitchModelRuntime("deepseek/alt"); err != nil {
		t.Fatal(err)
	}
	t.Run("model-switch", check)
	if err := b.ApplyConfiguration(b.Configuration()); err != nil {
		t.Fatal(err)
	}
	t.Run("configuration-rebuild", check)
}

func TestExtraToolsReachActualProviderRequest(t *testing.T) {
	t.Chdir(t.TempDir())
	type wireSnapshot struct {
		Tools    json.RawMessage   `json:"tools"`
		Messages []json.RawMessage `json:"messages"`
	}
	snapshots := make(chan wireSnapshot, 8)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var snapshot wireSnapshot
		if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(snapshot.Tools, &tools); err != nil {
			t.Error(err)
			http.Error(w, "bad tools", 400)
			return
		}
		select {
		case snapshots <- snapshot:
		case <-r.Context().Done():
			return
		}
		names := make(map[string]bool)
		for _, tool := range tools {
			names[tool.Function.Name] = true
		}
		for _, name := range []string{"capability", "agent_profiles", "skill", "deliver_file"} {
			if !names[name] {
				t.Errorf("provider request missing %s", name)
			}
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	b := newPersistTestBot(t)
	next := b.Configuration()
	next.Providers[0].BaseURL = server.URL + "/v1"
	next.Providers[0].Protocol = "openai"
	if err := b.ApplyConfiguration(next); err != nil {
		t.Fatal(err)
	}
	b.RegisterExtraTools(deliver.NewTool(nil))
	if err := b.SwitchModelRuntime("deepseek/alt"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	out, err := b.Run(ctx, "Which tools are available?", nil, &subagentDemoHost{})
	if err != nil || out != "ok" || calls.Load() != 1 {
		t.Fatalf("request failed: output=%q calls=%d err=%v", out, calls.Load(), err)
	}
	previous := <-snapshots
	for _, stage := range []string{"MCP state and repeated injection", "configuration rebuild"} {
		switch stage {
		case "MCP state and repeated injection":
			b.RegisterExtraTools(deliver.NewTool(nil))
			if err := b.ext.AddMCPServerBackground("prefix-fixture", mcp.ServerConfig{Command: "/nonexistent/nekocode-prefix-fixture"}); err != nil {
				t.Fatal(err)
			}
		case "configuration rebuild":
			if err := b.ApplyConfiguration(b.Configuration()); err != nil {
				t.Fatal(err)
			}
		}
		if out, err := b.Run(ctx, "Continue", nil, &subagentDemoHost{}); err != nil || out != "ok" {
			t.Fatalf("%s: output=%q err=%v", stage, out, err)
		}
		current := <-snapshots
		if !bytes.Equal(previous.Tools, current.Tools) {
			t.Fatalf("%s rewrote provider-visible tool schema", stage)
		}
		if len(current.Messages) <= len(previous.Messages) {
			t.Fatalf("%s did not append conversation", stage)
		}
		// context_data references are request-only tail messages. They are
		// regenerated after persistent history and are not an append-only prefix.
		prefixEnd := len(previous.Messages)
		for i, raw := range previous.Messages {
			var msg struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Data struct {
					Kind string `json:"kind"`
				} `json:"context_data"`
			}
			if json.Unmarshal([]byte(msg.Content), &envelope) == nil {
				switch envelope.Data.Kind {
				case "context_references", "evidence_references", "task_references":
					prefixEnd = i
				}
			}
			if prefixEnd != len(previous.Messages) {
				break
			}
		}
		if prefixEnd < 2 {
			t.Fatal("stable prefix unexpectedly empty")
		}
		for i := 0; i < prefixEnd; i++ {
			if !bytes.Equal(previous.Messages[i], current.Messages[i]) {
				t.Fatalf("%s rewrote provider message prefix at %d:\nbefore=%s\nafter=%s", stage, i, previous.Messages[i], current.Messages[i])
			}
		}
		if stage == "MCP state and repeated injection" {
			messages, _ := json.Marshal(current.Messages)
			if !bytes.Contains(messages, []byte("prefix-fixture")) {
				t.Fatal("live MCP state missing from request")
			}
		}
		previous = current
	}
}
