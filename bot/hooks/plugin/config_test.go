package plugin

import (
	"os"
	"testing"
)

func TestPluginSessionStartRunsOnce(t *testing.T) {
	root := t.TempDir()
	hooksJSON := `{
		"SessionStart": [{
			"matcher": ".*",
			"hooks": [{"type": "command", "command": "echo started"}]
		}]
	}`
	if err := os.WriteFile(root+"/hooks.json", []byte(hooksJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 1 {
		t.Fatalf("loaded %d hooks, want 1", len(loaded))
	}
	if !loaded[0].Once {
		t.Fatal("SessionStart hook should be marked once")
	}
	if got := loaded[0].On(Event{}); got == nil || got.Hint == nil {
		t.Fatalf("SessionStart hook result = %+v, want hint", got)
	}
}
