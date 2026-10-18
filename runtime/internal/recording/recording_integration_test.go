package recording

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekocode/runtime/internal/core"
	"nekocode/runtime/internal/runstore"
)

func TestEventRecorderWritesAndRestoresEvents(t *testing.T) {
	baseDir := t.TempDir()
	recorder, err := newTestRecorder(t, baseDir)
	if err != nil {
		t.Fatalf("NewEventRecorder: %v", err)
	}
	runID := core.RunID("run_1")
	now := time.Now()

	recorder.Record(core.Event{
		ID:     "evt_1",
		RunID:  runID,
		Type:   core.EventInputAccepted,
		Time:   now,
		Source: core.SourceRef{Kind: "telegram"},
		Payload: core.MessagePayload{
			Content: "edit README",
		},
	})
	diff := "--- a/README.md\n+++ b/README.md\n@@\n-old\n+new"
	recorder.Record(core.Event{
		ID:    "evt_2",
		RunID: runID,
		Type:  core.EventToolPreview,
		Time:  now.Add(time.Second),
		Payload: core.ToolPayload{
			ToolName: "edit",
			Preview:  diff,
		},
	})
	patch := "*** Begin Patch\n*** Update File: README.md\n@@\n-old\n+new\n*** End Patch"
	recorder.Record(core.Event{
		ID:    "evt_3",
		RunID: runID,
		Type:  core.EventToolPreview,
		Time:  now.Add(1500 * time.Millisecond),
		Payload: core.ToolPayload{
			ToolName: "apply_patch",
			Preview:  patch,
		},
	})
	review := "Findings\nSeverity: high\nMissing runtime artifact coverage."
	recorder.Record(core.Event{
		ID:    "evt_4",
		RunID: runID,
		Type:  core.EventToolCompleted,
		Time:  now.Add(1800 * time.Millisecond),
		Payload: core.ToolPayload{
			ToolName: "review",
			Output:   review,
		},
	})
	recorder.Record(core.Event{
		ID:    "evt_5",
		RunID: runID,
		Type:  core.EventRunDone,
		Time:  now.Add(2 * time.Second),
		Payload: core.RunResult{
			Output: "done",
		},
	})

	runDir := recorder.RunDir(runID)
	eventsData, err := os.ReadFile(filepath.Join(runDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(eventsData)), "\n") + 1; lines != 5 {
		t.Fatalf("events line count = %d, want 5\n%s", lines, eventsData)
	}
	if !strings.Contains(string(eventsData), `"payload_type":"runtime.MessagePayload"`) {
		t.Fatalf("payload type not recorded:\n%s", eventsData)
	}

	events, err := LoadRecordedEvents(baseDir)
	if err != nil {
		t.Fatalf("LoadRecordedEvents: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("loaded events = %d, want 5", len(events))
	}
	store := runstore.NewRunStore(0)
	for _, ev := range events {
		store.Record(ev)
	}
	run, ok := store.Lookup(runID)
	if !ok || run.Input != "edit README" || run.Output != "done" {
		t.Fatalf("restored run = %#v ok=%v", run, ok)
	}
}

func TestLoadRecentRecordedEventsLimitsRunFiles(t *testing.T) {
	baseDir := t.TempDir()
	recorder, err := newTestRecorder(t, baseDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		recorder.Record(core.Event{
			ID: fmt.Sprintf("evt_%d", i), RunID: core.RunID(fmt.Sprintf("run_%d", i)),
			Type: core.EventRunDone, Time: time.Unix(int64(i), 0),
			Payload: core.RunResult{Output: fmt.Sprintf("run %d", i)},
		})
	}

	events, err := LoadRecentRecordedEvents(baseDir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].RunID != "run_2" || events[1].RunID != "run_3" {
		t.Fatalf("recent events = %#v, want runs 2 and 3", events)
	}
}

func TestLoadRecentRecordedEventsUsesFileRecencyAcrossRecorderBatches(t *testing.T) {
	baseDir := t.TempDir()
	olderBatch := filepath.Join(baseDir, "20260101", runsSubdir, "run_1")
	newerBatch := filepath.Join(baseDir, "20260102", runsSubdir, "run_1")
	for _, dir := range []string{olderBatch, newerBatch} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeEvent := func(path, id string, eventTime time.Time) {
		t.Helper()
		data, err := json.Marshal(recordedEventFrom(core.Event{
			ID: id, RunID: core.RunID(id), Type: core.EventRunDone, Time: eventTime,
			Payload: core.RunResult{Output: id},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldPath := filepath.Join(olderBatch, "events.jsonl")
	newPath := filepath.Join(newerBatch, "events.jsonl")
	writeEvent(oldPath, "older_batch_recent_write", time.Unix(20, 0))
	writeEvent(newPath, "newer_batch_stale_write", time.Unix(10, 0))
	if err := os.Chtimes(newPath, time.Unix(10, 0), time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldPath, time.Unix(20, 0), time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}

	events, err := LoadRecentRecordedEvents(baseDir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].RunID != "older_batch_recent_write" {
		t.Fatalf("recent events = %#v", events)
	}
}

func TestRecordedV1SubAgentPayloadMigratesToV2(t *testing.T) {
	record := recordedEvent{
		Version: "1.0",
		Type:    core.EventSubAgentStarted,
		Payload: json.RawMessage(`{"tool_name":"research","args":"sub_1","output":"3"}`),
	}
	event, err := record.Event()
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := event.Payload.(core.SubAgentPayload)
	if !ok {
		t.Fatalf("payload type = %T", event.Payload)
	}
	if event.Version != core.ProtocolVersion ||
		payload.ID != "sub_1" || payload.Type != "research" || payload.Color != 3 {
		t.Fatalf("migrated event = %#v", event)
	}
}

func TestRecordedV1SessionEventMigratesToV2(t *testing.T) {
	event, err := (recordedEvent{
		Version: "1.0",
		Type:    core.EventType("session_resumed"),
	}).Event()
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != core.EventSessionChanged || event.Version != core.ProtocolVersion {
		t.Fatalf("migrated event = %#v", event)
	}
}
