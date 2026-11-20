package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	controlruntime "nekocode/runtime"
)

func TestPrompt(t *testing.T) {
	backend := &fakeBackend{
		current:  "session-1",
		sessions: []controlruntime.SessionMeta{{ID: "session-1", CWD: "/workspace"}},
		events:   make(chan controlruntime.Event, 2),
	}
	var output bytes.Buffer
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn)}
	s.conn = newConnection(&output)
	result, rpcErr := s.prompt(context.Background(), json.RawMessage(`{"sessionId":"session-1","prompt":[{"type":"text","text":"hello"}]}`))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if result.(map[string]any)["stopReason"] != "end_turn" || backend.input.Text != "hello" || !backend.waited {
		t.Fatalf("result = %#v, input = %#v", result, backend.input)
	}
	if !strings.Contains(output.String(), `"sessionUpdate":"agent_message_chunk"`) {
		t.Fatalf("missing message update in %q", output.String())
	}
	select {
	case <-backend.eventCtx.Done():
	default:
		t.Fatal("prompt event subscription was not released")
	}
}

type evidenceFailureBackend struct {
	fakeBackend
	kind   controlruntime.EventType
	result controlruntime.RunResult
}

func (f *evidenceFailureBackend) StartRun(context.Context, controlruntime.Input) (controlruntime.RunID, error) {
	f.events <- controlruntime.Event{RunID: "run-1", Type: f.kind, Payload: f.result}
	return "run-1", nil
}
func TestPromptPreservesIncompleteEvidenceMetadata(t *testing.T) {
	for _, kind := range []controlruntime.EventType{controlruntime.EventRunFailed, controlruntime.EventRunCancelled, controlruntime.EventRunDone} {
		t.Run(string(kind), func(t *testing.T) {
			backend := &evidenceFailureBackend{fakeBackend: fakeBackend{current: "session-1", events: make(chan controlruntime.Event, 1)}, kind: kind, result: controlruntime.RunResult{Error: "save failed", EvidenceIncomplete: true}}
			var output bytes.Buffer
			s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(&output)}
			result, rpcErr := s.prompt(t.Context(), json.RawMessage(`{"sessionId":"session-1","prompt":[{"type":"text","text":"run"}]}`))
			if kind == controlruntime.EventRunFailed {
				if rpcErr == nil || rpcErr.Code != -32603 || rpcErr.Data.(map[string]any)["nekocode/evidence_incomplete"] != true {
					t.Fatalf("error=%+v", rpcErr)
				}
			} else {
				if rpcErr != nil || result.(map[string]any)["_meta"].(map[string]any)["nekocode/evidence_incomplete"] != true {
					t.Fatalf("result=%+v error=%+v", result, rpcErr)
				}
			}
			if !backend.waited {
				t.Fatal("terminal returned before runner settlement")
			}
		})
	}
	if _, exists := terminalPromptResult("cancelled", controlruntime.RunResult{})["_meta"]; exists {
		t.Fatal("legacy result gained metadata")
	}
}

type delayedSettlementBackend struct {
	fakeBackend
	settled bool
}

func (b *delayedSettlementBackend) WaitRun(ctx context.Context, id controlruntime.RunID) error {
	if !b.settled {
		return context.DeadlineExceeded
	}
	return b.fakeBackend.WaitRun(ctx, id)
}

func TestPromptRetriesSettlementBeforeAdmittingNextTurn(t *testing.T) {
	b := &delayedSettlementBackend{fakeBackend: fakeBackend{current: "s", events: make(chan controlruntime.Event, 2)}}
	s := &server{backend: b, active: make(map[string]*activeTurn), conn: newConnection(&bytes.Buffer{})}
	req := json.RawMessage(`{"sessionId":"s","prompt":[{"type":"text","text":"run"}]}`)
	if _, err := s.prompt(t.Context(), req); err == nil || err.Code != -32603 {
		t.Fatalf("cleanup: %+v", err)
	}
	if _, err := s.prompt(t.Context(), req); err == nil || err.Code != -32000 {
		t.Fatalf("unsettled admission: %+v", err)
	}
	b.settled = true
	if _, err := s.prompt(t.Context(), req); err != nil {
		t.Fatalf("settled retry: %+v", err)
	}
}

type rejectedAdmissionBackend struct{ fakeBackend }

func (b *rejectedAdmissionBackend) StartRun(context.Context, controlruntime.Input) (controlruntime.RunID, error) {
	return "", context.Canceled
}
func TestPromptOnlyCommitsImagesAfterAdmission(t *testing.T) {
	for _, admitted := range []bool{true, false} {
		t.Setenv("HOME", t.TempDir())
		fake := &fakeBackend{current: "s", events: make(chan controlruntime.Event, 2)}
		var backend Backend = fake
		if !admitted {
			backend = &rejectedAdmissionBackend{*fake}
		}
		s := &server{backend: backend, active: make(map[string]*activeTurn), conn: newConnection(&bytes.Buffer{})}
		if _, err := s.prompt(t.Context(), json.RawMessage(`{"sessionId":"s","prompt":[{"type":"image","data":"iVBORw0KGgo="}]}`)); err != nil {
			t.Fatal(err)
		}
		if !admitted {
			requireNoDraftImages(t, "s")
			continue
		}
		if len(fake.input.Images) != 1 {
			t.Fatalf("missing image: %+v", fake.input)
		}
		if _, err := os.Stat(fake.input.Images[0].Path); err != nil {
			t.Fatalf("admitted image: %v", err)
		}
	}
}
