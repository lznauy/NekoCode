package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackgroundInboxPersistsAcceptanceAndRequiresExplicitRecovery(t *testing.T) {
	root := t.TempDir()
	started := make(chan struct{})
	rt := New(RunnerFunc(func(ctx context.Context, _ string, _ RunHost) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}), backgroundServices(root))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := rt.StartRun(ctx, Input{Text: "hold coordinator"}); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	id, err := rt.StartRun(ctx, Input{Text: "durable next demand", Source: SourceRef{Kind: "gui_queue"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "session", "background.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved backgroundState
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Demands) != 1 || saved.Demands[0].ID != id || saved.Demands[0].Status != "queued" {
		t.Fatalf("receipt not durable: %+v", saved)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	ran := make(chan struct{}, 1)
	recovered := New(RunnerFunc(func(_ context.Context, input string, _ RunHost) (string, error) {
		if input != "durable next demand" {
			t.Errorf("wrong demand: %q", input)
		}
		ran <- struct{}{}
		return "done", nil
	}), backgroundServices(root))
	defer recovered.Close()
	inbox, err := recovered.BackgroundDemands("session")
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 || inbox[0].Status != "pending" {
		t.Fatalf("not recovered as pending: %+v", inbox)
	}
	select {
	case <-ran:
		t.Fatal("replayed without explicit resume")
	default:
	}
	if err := recovered.ResumeBackgroundDemand(ctx, "other", id); err == nil {
		t.Fatal("cross-session resume accepted")
	}
	if err := recovered.ResumeBackgroundDemand(ctx, "session", id); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, ran)
	if err := recovered.WaitRun(ctx, id); err != nil {
		t.Fatal(err)
	}
	inbox, err = recovered.BackgroundDemands("session")
	if err != nil || inbox[0].Status != "done" {
		t.Fatalf("receipt not settled: %+v %v", inbox, err)
	}
	if err := recovered.ResumeBackgroundDemand(ctx, "session", id); err == nil {
		t.Fatal("completed demand replayed")
	}
}

func TestBackgroundInboxNeverReplaysClaimedTurn(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "session")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(backgroundState{Version: 2, Demands: []BackgroundDemand{{ID: "queued_claimed", SessionID: "session", Input: Input{Text: "may have modified files"}, Status: "claimed"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "background.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		t.Error("claimed turn replayed")
		return "", nil
	}), backgroundServices(root))
	defer rt.Close()
	inbox, err := rt.BackgroundDemands("session")
	if err != nil {
		t.Fatal(err)
	}
	if inbox[0].Status != "interrupted" {
		t.Fatalf("unsafe recovery: %+v", inbox)
	}
	if err := rt.ResumeBackgroundDemand(t.Context(), "session", inbox[0].ID); err == nil {
		t.Fatal("claimed turn resumed")
	}
	if err := rt.DiscardBackgroundDemand(t.Context(), "other", inbox[0].ID); err == nil {
		t.Fatal("cross-session discard")
	}
	if err := rt.DiscardBackgroundDemand(t.Context(), "session", inbox[0].ID); err != nil {
		t.Fatal(err)
	}
	inbox, err = rt.BackgroundDemands("session")
	if err != nil || inbox[0].Status != "cancelled" {
		t.Fatalf("discard not persisted: %+v %v", inbox, err)
	}
}

func TestBackgroundInboxAdmissionFailureDoesNotCreateGhostReceipt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "session"), []byte("blocked directory"), 0600); err != nil {
		t.Fatal(err)
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		t.Error("failed acceptance executed")
		return "", nil
	}), backgroundServices(root))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	b.loaded["session"] = true
	b.mu.Unlock()
	if _, err := rt.queueBackgroundInput(t.Context(), Input{Text: "must not acknowledge"}); err == nil {
		t.Fatal("storage failure ignored")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) != 0 || len(b.inputs) != 0 || len(b.demands) != 0 || len(b.demandOrder) != 0 {
		t.Fatal("failed admission left a ghost")
	}
}

func TestBackgroundInboxSerializedSizeCannotExceedRecoveryLimit(t *testing.T) {
	root := t.TempDir()
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(root))
	defer rt.Close()
	b := rt.background
	if err := b.load("session"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "session", "background.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	for i := range 90 {
		id := RunID(fmt.Sprintf("queued_%d", i))
		b.demands[id] = &BackgroundDemand{ID: id, SessionID: "session", Input: Input{Text: strings.Repeat("\x00", backgroundDemandTextLimit)}, Status: "pending"}
		b.demandOrder = append(b.demandOrder, id)
	}
	err = b.saveLocked("session")
	b.demands = map[RunID]*BackgroundDemand{}
	b.demandOrder = nil
	b.mu.Unlock()
	if err == nil {
		t.Fatal("oversized serialized state accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("oversized write changed the last valid state")
	}
}
