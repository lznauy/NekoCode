package core

import (
	"context"
	"errors"
	"testing"

	"nekocode/mode"
	"nekocode/protocol"
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
