package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
	"nekocode/protocol"
)

// Opt-in acceptance with the user's configured model. Only a generated fixture
// is sent; config, sessions and any tool state are isolated in a temporary home.
func TestSubagentLiveModelAcceptance(t *testing.T) {
	if os.Getenv("NEKOCODE_SUBAGENT_LIVE") != "1" {
		t.Skip("set NEKOCODE_SUBAGENT_LIVE=1 to run real model acceptance")
	}
	configured, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	model := configured.ActiveModelConfig()
	if model.APIKey == "" {
		t.Fatal("active model has no API key")
	}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	model.Name = "live-acceptance"
	if err := config.Save(config.Config{Active: "live/" + model.Name, Providers: []config.ProviderConfig{{Name: "live", APIKey: model.APIKey, BaseURL: model.BaseURL, Protocol: model.Protocol, Models: []config.ModelDefinition{{Name: model.Name, Model: model.Model, ContextWindow: model.ContextWindow, ReasoningEffort: model.ReasoningEffort}}}}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "probe.txt")
	const fixture = "verification_token=NEKO_ACCEPTANCE_527\n"
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	tool, err := b.toolbox.Registry.Get("task")
	if err != nil {
		t.Fatal(err)
	}
	host := &subagentDemoHost{}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		args := map[string]any{
			"profile": "explore", "prompt": "Read " + path + " and report its exact verification_token with file evidence. This is a read-only acceptance check.",
			"_sub_callback": taskbridge.TaskCallback{ID: "live-probe", Callback: func(ev protocol.StepEvent) { ev.SubAgentID = "live-probe"; host.Step(ev) }},
		}
		if attempt > 0 {
			args["resume_id"] = "live-probe"
			args["prompt"] = "Using the earlier file evidence in your saved context, repeat the exact verification_token. Do not read or modify any files."
		}
		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("attempt %d failed: %v", attempt+1, err)
		}
		if !strings.Contains(result.Text, "NEKO_ACCEPTANCE_527") || !strings.Contains(result.Text, "[Subagent ID: live-probe]") {
			t.Fatalf("attempt %d lost evidence or identity", attempt+1)
		}
	}
	metadata, err := session.ReadSubagentMetadata(b.sess.CurrentID(), "live-probe")
	if err != nil || metadata.RunCount != 2 || metadata.Status != "completed" {
		t.Fatal("real-model task did not persist two completed attempts", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != fixture {
		t.Fatal("read-only fixture changed", err)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	var reads, starts, ends int
	for _, event := range host.events {
		if event.Action == protocol.StepActionSubAgentStart {
			starts++
		}
		if event.Action == protocol.StepActionSubAgentEnd {
			ends++
		}
		if event.Action == protocol.StepActionExecuteTool && event.ToolName == "read" && !event.IsError {
			reads++
		}
	}
	if starts != 2 || ends != 2 || reads < 1 {
		t.Fatalf("incomplete real lifecycle: starts=%d ends=%d reads=%d", starts, ends, reads)
	}
	t.Log("real model: read evidence, structured completion, persisted history and resume verified")
}
