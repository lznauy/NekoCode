package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"nekocode/bot/command"
	"nekocode/mode"
	"nekocode/protocol"
	"nekocode/util/fs"
)

func TestInteractionModePersistenceIsolationAndUnknownRecovery(t *testing.T) {
	b := newPersistTestBot(t)
	b.ctxMgr.Add("user", "saved conversation")
	if err := b.SetInteractionMode(mode.Plan); err != nil {
		t.Fatal(err)
	}
	id := b.CurrentSessionID()
	saved, err := b.sess.Load(id)
	if err != nil || saved.InteractionMode != "plan" {
		t.Fatalf("saved mode: %+v %v", saved, err)
	}
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	if b.InteractionMode() != mode.Standard {
		t.Fatal("new session inherited plan")
	}
	if err := b.ResumeSession(id); err != nil {
		t.Fatal(err)
	}
	if b.InteractionMode() != mode.Plan {
		t.Fatal("resume lost plan selection")
	}
	saved.InteractionMode = "future-mode"
	if err := b.sess.Save(saved); err != nil {
		t.Fatal(err)
	}
	if err := b.ResumeSession(id); err != nil {
		t.Fatal(err)
	}
	if b.InteractionMode() != "future-mode" {
		t.Fatal("unknown mode silently downgraded")
	}
	if _, err := b.ResolveInteractionMode("execute"); err == nil {
		t.Fatal("unknown mode executable")
	}
	result, err := b.ExecuteCommand(context.Background(), "/mode standard", &subagentDemoHost{})
	if err != nil || result.Action != protocol.CommandHandled || b.InteractionMode() != mode.Standard {
		t.Fatalf("cannot recover unknown mode: %+v %v", result, err)
	}
}

func TestInteractionModeOneShotAndBusyBoundary(t *testing.T) {
	b := newPersistTestBot(t)
	snapshot, err := b.ResolveInteractionMode("/plan inspect")
	if err != nil || snapshot.ID != mode.Plan || b.InteractionMode() != mode.Standard {
		t.Fatalf("one-shot changed selection: %+v %v", snapshot, err)
	}
	b.operationMu.Lock()
	err = b.SetInteractionMode(mode.Plan)
	b.operationMu.Unlock()
	if err == nil || b.InteractionMode() != mode.Standard {
		t.Fatal("mode switch raced a running operation")
	}
	if err := b.SetInteractionMode(mode.Plan); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Run(ctx, "inspect", nil, &subagentDemoHost{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run: %v", err)
	}
	if b.InteractionMode() != mode.Plan {
		t.Fatal("run cleanup cleared session selection")
	}
	if b.getAgent().Executor().AccessObservation().PlanMode {
		t.Fatal("run policy leaked after cleanup")
	}
}

func TestInteractionModeKeepsEmptyDraftIdentity(t *testing.T) {
	for _, command := range []bool{false, true} {
		t.Run(fmt.Sprintf("command=%v", command), func(t *testing.T) {
			b := newPersistTestBot(t)
			id := b.CurrentSessionID()
			if command {
				if _, err := b.ExecuteCommand(t.Context(), "/mode plan", &subagentDemoHost{}); err != nil {
					t.Fatal(err)
				}
			} else if err := b.SetInteractionMode(mode.Plan); err != nil {
				t.Fatal(err)
			}
			if b.CurrentSessionID() != id {
				t.Fatalf("selecting Plan discarded draft identity: got %q, want %q", b.CurrentSessionID(), id)
			}
			// Another empty control-plane save must preserve the preference too.
			if _, err := b.ExecuteCommand(t.Context(), "/help", &subagentDemoHost{}); err != nil {
				t.Fatal(err)
			}
			if b.CurrentSessionID() != id || b.InteractionMode() != mode.Plan {
				t.Fatal("control command discarded Plan draft")
			}
			if len(b.ListSessions()) != 0 {
				t.Fatal("empty draft appeared in saved conversations")
			}
		})
	}
}

func TestSessionSwitchSaveFailureKeepsTargetMode(t *testing.T) {
	b := newPersistTestBot(t)
	b.ctxMgr.Add("user", "Plan session")
	if err := b.SetInteractionMode(mode.Plan); err != nil {
		t.Fatal(err)
	}
	target := b.CurrentSessionID()
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	b.ctxMgr.Add("user", "Standard session")
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(fs.NekocodeDataDir("sessions"), target)
	manifest := filepath.Join(directory, "session.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Inject a disk failure after the same switch used by /sessions, before
	// ExecuteCommand's trailing persistence. This does not depend on OS uid.
	b.cmd.Parser().RegisterInfo("test-resume", "", func(ctx context.Context, _ *command.Command) (string, bool) {
		if err := b.ResumeSession(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(manifest); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(manifest, 0700); err != nil {
			t.Fatal(err)
		}
		return "", true
	})
	defer func() { _ = os.Remove(manifest); _ = os.WriteFile(manifest, data, 0600) }()
	if _, err := b.ExecuteCommand(t.Context(), "/test-resume", &subagentDemoHost{}); err == nil {
		t.Fatal("expected trailing save failure")
	}

	if b.CurrentSessionID() != target {
		t.Fatal("fixture did not switch to target before save failure")
	}
	if b.InteractionMode() != mode.Plan {
		t.Fatalf("target mode rolled back to source mode %q", b.InteractionMode())
	}
}

func TestModeSaveFailureRollsBackWithinSameSession(t *testing.T) {
	for _, command := range []bool{false, true} {
		t.Run(fmt.Sprintf("command=%v", command), func(t *testing.T) {
			b := newPersistTestBot(t)
			b.ctxMgr.Add("user", "Plan session")
			if err := b.SetInteractionMode(mode.Plan); err != nil {
				t.Fatal(err)
			}
			id := b.CurrentSessionID()
			manifest := filepath.Join(fs.NekocodeDataDir("sessions"), id, "session.json")
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(manifest); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(manifest, 0700); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.Remove(manifest); _ = os.WriteFile(manifest, data, 0600) }()
			if command {
				_, err = b.ExecuteCommand(t.Context(), "/mode standard", &subagentDemoHost{})
			} else {
				err = b.SetInteractionMode(mode.Standard)
			}
			if err == nil {
				t.Fatal("expected persistence failure")
			}
			if b.CurrentSessionID() != id || b.InteractionMode() != mode.Plan {
				t.Fatal("failed mutation relaxed Plan or changed session")
			}
		})
	}
}
