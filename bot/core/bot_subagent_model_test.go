package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
)

func TestResolveSubagentModel(t *testing.T) {
	parent := config.ModelConfig{Name: "main", Model: "gpt-5", ReasoningEffort: "high", ContextWindow: 100000}
	other := config.ModelConfig{Name: "worker", Model: "other", ContextWindow: 64000}
	cfg := config.Config{Providers: []config.ProviderConfig{{Name: "test", Models: []config.ModelDefinition{
		{Name: parent.Name, Model: parent.Model, ReasoningEffort: parent.ReasoningEffort, ContextWindow: parent.ContextWindow},
		{Name: other.Name, Model: other.Model, ContextWindow: other.ContextWindow},
	}}}}
	parent.Name = "test/main"
	other.Name = "test/worker"
	cfg.Active, cfg.CompactionModel = parent.Name, other.Name
	for _, tc := range []struct {
		name string
		want config.ModelConfig
	}{{"", parent}, {"test/worker", other}, {"test/main", parent}} {
		got, err := resolveSubagentModel(&cfg, parent, tc.name)
		if err != nil || got.Name != tc.want.Name || effectiveModelWindow(got) != tc.want.ContextWindow {
			t.Fatalf("%+v %v", got, err)
		}
	}
	if _, err := resolveSubagentModel(&cfg, parent, "gpt-5"); err == nil {
		t.Fatal("raw provider model name accepted")
	}
}

func TestSubagentModelsConcurrentAndResume(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	dir := filepath.Join(root, ".nekocode", "plugins", "workers")
	for _, sub := range []string{".claude-plugin", "agents"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name":"workers","agents":["agents/fast.md","agents/deep.md","agents/bad.md"]}`)
	profile := func(name, model string) string {
		return "---\nname: " + name + "\nbase: explore\ntools: []\nmodel: " + model + "\n---\nFinish the requested inspection."
	}
	for name, model := range map[string]string{"fast": "fast", "deep": "deep", "bad": "missing"} {
		write(filepath.Join(dir, "agents", name+".md"), profile(name, "openai/"+model))
	}
	var mu sync.Mutex
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model           string `json:"model"`
			MaxTokens       int    `json:"max_tokens"`
			ReasoningEffort string `json:"reasoning_effort"`
			Tools           []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "bad", 400)
			return
		}
		mu.Lock()
		counts[req.Model]++
		mu.Unlock()
		if req.Model == "flash" {
			t.Error("work request used compaction model")
		}
		if req.MaxTokens <= 0 {
			t.Error("missing output limit")
		}
		if req.Model != "main" && (len(req.Tools) != 1 || req.Tools[0].Function.Name != "nekocode_submit_result") {
			t.Errorf("tool ceiling changed: %+v", req.Tools)
		}
		if req.Model == "gpt-5" && req.ReasoningEffort != "high" {
			t.Errorf("selected model reasoning lost: %q", req.ReasoningEffort)
		}
		args, _ := json.Marshal(map[string]any{"summary": "done with " + req.Model, "evidence": []string{"local model fixture"}, "files": []string{}, "verification": "fixture", "unfinished": []string{}, "risks": []string{}})
		delta := map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "finish", "type": "function", "function": map[string]any{"name": "nekocode_submit_result", "arguments": string(args)}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []any{map[string]any{"index": 0, "delta": delta}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}} {
			data, _ := json.Marshal(map[string]any{"choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := config.Config{Active: "openai/main", CompactionModel: "openai/flash", Providers: []config.ProviderConfig{{Name: "openai", APIKey: "fixture", BaseURL: server.URL + "/v1", Protocol: "openai"}}}
	for name, window := range map[string]int{"main": 128000, "fast": 64000, "deep": 96000, "flash": 32000} {
		m := config.ModelConfig{Name: name, Provider: "openai", Model: name, Protocol: "openai", APIKey: "fixture", BaseURL: server.URL + "/v1", ContextWindow: window}
		if name == "deep" {
			m.Model = "gpt-5"
			m.ReasoningEffort = "high"
		}
		cfg.Providers[0].Models = append(cfg.Providers[0].Models, config.ModelDefinition{Name: m.Name, Model: m.Model, ContextWindow: m.ContextWindow, ReasoningEffort: m.ReasoningEffort})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	task, err := b.toolbox.Registry.Get("task")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := b.toolbox.Registry.Get("agent_profiles")
	if err != nil {
		t.Fatal(err)
	}
	catalogOut, err := catalog.Execute(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Profiles []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(catalogOut.Render()), &listed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range listed.Profiles {
		if p.ID == "workers/fast" && p.Model == "openai/fast" {
			found = true
		}
	}
	if !found {
		t.Fatal("catalog lost model override")
	}
	run := func(id, name string, resume bool) {
		t.Helper()
		args := map[string]any{"prompt": "Inspect the supplied evidence and finish."}
		if resume {
			args["resume_id"] = id
		} else {
			args["profile"] = name
		}
		out, err := task.Execute(taskbridge.WithTaskID(t.Context(), id), args)
		if err != nil || !strings.Contains(out.Text, "done with") {
			t.Errorf("task %s: %s %v", id, out.Text, err)
		}
	}
	var wg sync.WaitGroup
	for _, name := range []string{"fast", "deep", "explore"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := name
			if name != "explore" {
				p = "workers/" + name
			}
			run(name, p, false)
		}()
	}
	wg.Wait()
	// Resume adopts the current trusted profile, not an old model/window from history.
	write(filepath.Join(dir, "agents", "fast.md"), profile("fast", "openai/deep"))
	b.ext.Reload()
	run("fast", "", true)
	child, err := session.OpenSubagent(b.sess.CurrentID(), "fast", root, true)
	if err != nil {
		t.Fatal(err)
	}
	snap := child.Snapshot()
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if snap.ContextWindow != 96000 || snap.Subagent.CurrentAttempt().ContextWindow != 96000 {
		t.Fatalf("resume retained old window: %+v", snap.Subagent.CurrentAttempt())
	}
	_, err = task.Execute(taskbridge.WithTaskID(t.Context(), "bad"), map[string]any{"profile": "workers/bad", "prompt": "inspect"})
	if err == nil || !strings.Contains(err.Error(), "unknown subagent model") {
		t.Fatalf("unknown model: %v", err)
	}
	if _, err := session.ReadSubagentMetadata(b.sess.CurrentID(), "bad"); !os.IsNotExist(err) {
		t.Fatalf("unknown model created execution history: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["main"] != 1 || counts["fast"] != 1 || counts["gpt-5"] != 2 || counts["flash"] != 0 {
		t.Fatalf("wrong model routing: %v", counts)
	}
}
