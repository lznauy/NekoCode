package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// These replay tests validate comparison/scoring plumbing, not model behavior.
func TestBehaviorComparisonIsolation(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"bot/prompt/system_zh.md":                    "archived-system",
		"bot/extension/skill/bundled/hunt/SKILL.md":  "---\nname: hunt\ndescription: archived diagnosis\n---\narchived-hunt",
		"bot/extension/skill/bundled/check/SKILL.md": "---\nname: check\ndescription: archived review\n---\narchived-check",
	}
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=eval", "-c", "user.email=eval@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "baseline"},
	} {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("prepare baseline: %v: %s", err, output)
		}
	}
	baseline, commit := behaviorVariantAt(t, root, "HEAD")
	current := describeBehaviorVariant(t, "current", "candidate-system", &workflowSet{skills: map[string]*workflow{"hunt": {Name: "hunt", Content: "candidate-hunt"}, "check": {Name: "check", Content: "candidate-check"}}})
	if commit == "" || baseline.version.SystemSHA256 == current.version.SystemSHA256 {
		t.Fatal("comparison did not identify separate versions")
	}
	for _, name := range []string{"hunt", "check"} {
		old, _ := baseline.manager.Get(name)
		now, _ := current.manager.Get(name)
		if old.Content != "archived-"+name || old.Content == now.Content {
			t.Fatalf("%s skill mixed versions or leaked archive paths", name)
		}
	}
	for _, variant := range []behaviorVariant{baseline, current} {
		t.Run(variant.version.Name, func(t *testing.T) {
			sk, _ := variant.manager.Get("hunt")
			client := &behaviorReplay{system: variant.system, workflow: sk.Content}
			result := runBehaviorCase(t, client, variant, behaviorCases()[2])
			if result.InfrastructureError != "" || len(result.Issues) != 0 || !client.sawFollowup {
				t.Fatalf("comparison replay failed: %+v, followup=%v", result, client.sawFollowup)
			}
			// A successful repair is still a violation of diagnosis-only scope.
			client = &behaviorReplay{system: variant.system, workflow: sk.Content}
			result = runBehaviorCase(t, client, variant, behaviorCases()[0])
			if !slices.Contains(result.Issues, "unauthorized_edit_attempt") {
				t.Fatalf("unauthorized repair scored as success: %+v", result)
			}
		})
	}
}

type behaviorReplay struct {
	system, workflow string
	step             int
	sawFollowup      bool
}

func (c *behaviorReplay) Chat(_ context.Context, messages []message, _ []toolDef) (*modelResponse, error) {
	if len(messages) == 0 || messages[0].Content != c.system {
		return nil, fmt.Errorf("system prompt mixed versions")
	}
	if c.step == 1 && !strings.Contains(messages[len(messages)-1].Content, c.workflow) {
		return nil, fmt.Errorf("skill content mixed versions")
	}
	for _, message := range messages {
		if message.Role == "user" && strings.HasPrefix(message.Content, "补充：") {
			c.sawFollowup = true
		}
	}
	sequence := []struct {
		tool string
		args map[string]string
	}{
		{"skill", map[string]string{"name": "hunt"}},
		{"read", map[string]string{"path": "add.go"}},
		{"shell", map[string]string{"command": "go test ./..."}},
		{"edit", map[string]string{"path": "add.go", "old_string": "a - b", "new_string": "a + b"}},
		{"shell", map[string]string{"command": "go test ./..."}},
	}
	answer := message{Role: "assistant", Content: "fixture replay finished"}
	if c.step < len(sequence) {
		item := sequence[c.step]
		data, _ := json.Marshal(item.args)
		answer.Content = ""
		answer.ToolCalls = []toolCall{{ID: fmt.Sprint(c.step), Type: "function", Function: functionCall{Name: item.tool, Arguments: string(data)}}}
	}
	c.step++
	return &modelResponse{Choices: []choice{{Message: answer}}}, nil
}
