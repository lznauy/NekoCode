package core

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"nekocode/bot/config"
)

// seedPersistConfig writes a two-model config so SwitchModel has a target.
// Both models use deepseek-v4-flash, whose built-in profile supports
// non-auto reasoning efforts (needed by the effort persistence test).
func seedPersistConfig(t *testing.T) {
	t.Helper()
	cfg := config.Config{
		Active:    "deepseek/default",
		Providers: []config.ProviderConfig{{Name: "deepseek", APIKey: "sk-test", Models: []config.ModelDefinition{{Name: "default", Model: "deepseek-v4-flash"}, {Name: "alt", Model: "deepseek-v4-flash"}}}},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func newPersistTestBot(t *testing.T) *Bot {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	seedPersistConfig(t)
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	return b
}

func TestSwitchModelPersistsConfig(t *testing.T) {
	b := newPersistTestBot(t)

	if err := b.SwitchModel("deepseek/alt"); err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if loaded.Active != "deepseek/alt" {
		t.Fatalf("persisted active = %q, want %q", loaded.Active, "deepseek/alt")
	}
}

func TestSetReasoningEffortPersistsConfig(t *testing.T) {
	b := newPersistTestBot(t)

	if err := b.SetReasoningEffort("high"); err != nil {
		t.Fatalf("SetReasoningEffort: %v", err)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got := loaded.ActiveModelConfig().ReasoningEffort; got != "high" {
		t.Fatalf("persisted reasoning effort = %q, want %q", got, "high")
	}
}

func TestRuntimeModelChangesDoNotPersistConfig(t *testing.T) {
	b := newPersistTestBot(t)
	if err := b.SwitchModelRuntime("deepseek/alt"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetReasoningEffortRuntime("high"); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active != "deepseek/default" || loaded.ActiveModelConfig().ReasoningEffort != "" {
		t.Fatalf("runtime-only change leaked to disk: active=%q effort=%q", loaded.Active, loaded.ActiveModelConfig().ReasoningEffort)
	}
}

func TestRuntimeReasoningRebuildPreservesFullAccess(t *testing.T) {
	b := newPersistTestBot(t)
	b.SetFullAccess(true)

	if err := b.SetReasoningEffortRuntime("high"); err != nil {
		t.Fatal(err)
	}
	if !b.FullAccess() || !b.getAgent().Executor().FullAccess() {
		t.Fatal("agent rebuild disabled full access")
	}
}

func TestPersistedModelChangesLeaveRuntimeUnchangedOnSaveFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*Bot) error
	}{
		{"model", func(b *Bot) error { return b.SwitchModel("deepseek/alt") }},
		{"reasoning", func(b *Bot) error { return b.SetReasoningEffort("high") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newPersistTestBot(t)
			previous := b.Configuration()
			originalAgent := b.getAgent()
			originalHome := os.Getenv("HOME")
			blocker := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(blocker, []byte("blocked"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", blocker)
			err := tc.apply(b)
			t.Setenv("HOME", originalHome)
			var pathError *os.PathError
			if !errors.As(err, &pathError) {
				t.Fatalf("save failure lost filesystem error: %v", err)
			}
			if !reflect.DeepEqual(b.Configuration(), previous) {
				t.Error("failed save changed in-memory configuration")
			}
			if b.getAgent() != originalAgent {
				t.Error("failed save rebuilt the agent")
			}
			saved, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if saved.Active != previous.Active || saved.ActiveModelConfig().ReasoningEffort != previous.ActiveModelConfig().ReasoningEffort {
				t.Error("failed save changed persisted model settings")
			}
		})
	}
}
