package recording

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nekocode/runtime/internal/core"
)

func TestRunRecordingErrorsRemainScopedThroughTerminalClose(t *testing.T) {
	for _, mode := range []string{"prior_write", "terminal_write", "close", "bind", "encode"} {
		t.Run(mode, func(t *testing.T) {
			rec, err := newTestRecorder(t, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := rec.BeginRun("broken", "scope"); err != nil {
				t.Fatal(err)
			}
			if err := rec.RecordRunEvent(core.Event{RunID: "broken", Type: core.EventRunStarted}); err != nil {
				t.Fatal(err)
			}
			run := rec.runs["broken"]
			path := filepath.Join(run.dir, "events.jsonl")
			switch mode {
			case "prior_write", "terminal_write":
				full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
				if err != nil {
					t.Skip("requires /dev/full")
				}
				defer full.Close()
				good := run.events
				run.events = full
				if mode == "prior_write" {
					if err := rec.RecordRunEvent(core.Event{RunID: "broken", Type: core.EventAssistantMessage, Payload: core.MessagePayload{Content: "already produced"}}); err == nil {
						t.Fatal("expected ENOSPC")
					}
					run.events = good
				} else {
					defer good.Close()
				}
			case "encode":
				if err := rec.RecordRunEvent(core.Event{RunID: "broken", Type: core.EventToolCompleted, Payload: core.ToolPayload{Input: json.RawMessage("{")}}); err == nil {
					t.Fatal("invalid payload silently recorded")
				}
			case "close":
				if err := run.lock.Close(); err != nil {
					t.Fatal(err)
				}
			case "bind":
				run.provisional = true
				if err := os.MkdirAll(filepath.Join(rec.baseDir, "target", runsSubdir, "broken"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := rec.BindRunSession("broken", "target"); err == nil {
					t.Fatal("expected binding collision")
				}
			}
			result := core.Event{RunID: "broken", Type: core.EventRunDone, Payload: core.RunResult{Output: "actual output"}}
			terminalErr := rec.RecordRunEvent(result)
			if terminalErr == nil || run.writeErr == nil {
				t.Fatal("terminal lost recording failure")
			}
			if mode == "close" && !errors.Is(terminalErr, os.ErrClosed) {
				t.Fatalf("close cause lost: %v", terminalErr)
			}
			if run.events != nil || run.lock != nil {
				t.Fatal("terminal did not release handles")
			}
			if mode == "prior_write" || mode == "bind" || mode == "encode" {
				events, err := loadRecordedEventFile(path)
				if err != nil {
					t.Fatal(err)
				}
				terminal := events[len(events)-1].Payload.(core.RunResult)
				if !terminal.EvidenceIncomplete || terminal.Output != "actual output" {
					t.Fatalf("persisted result=%+v", terminal)
				}
			}
			rec.EndRun("broken")
			if err := rec.BeginRun("healthy", "scope"); err != nil {
				t.Fatal(err)
			}
			if err := rec.RecordRunEvent(core.Event{RunID: "healthy", Type: core.EventRunDone, Payload: core.RunResult{Output: "next"}}); err != nil {
				t.Fatalf("earlier run contaminated healthy recording: %v", err)
			}
			rec.EndRun("healthy")
			if rec.Close() == nil {
				t.Fatal("Close lost earlier recording failure")
			}
		})
	}
}
