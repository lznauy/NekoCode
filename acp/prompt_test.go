package acp

import (
	"bytes"
	"context"
	"encoding/json"
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
	for _, kind := range []controlruntime.EventType{controlruntime.EventRunFailed, controlruntime.EventRunCancelled} {
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
