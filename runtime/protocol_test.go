package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type blockingRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingRunner) Run(context.Context, string, RunHost) (string, error) {
	close(r.started)
	<-r.release
	return "done", nil
}

func TestProtocolManifestAndBusyError(t *testing.T) {
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	rt := New(runner, ServiceGroups{})
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})

	if got := rt.Capabilities(); got.Protocol != ProtocolVersion || got.Steering {
		t.Fatalf("capabilities = %+v", got)
	}
	runID, err := rt.StartRun(context.Background(), Input{Text: "first", Source: SourceRef{Kind: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	gotID, err := rt.StartRun(context.Background(), Input{Text: "second", Source: SourceRef{Kind: "test"}})
	if gotID != runID {
		t.Fatalf("busy run id = %q, want %q", gotID, runID)
	}
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != ErrorBusy {
		t.Fatalf("busy error = %#v", err)
	}
	close(runner.release)
}

func TestEventsCarryVersionSequenceAndCursor(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		return "done", nil
	}), ServiceGroups{})
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	runID, err := rt.StartRun(context.Background(), Input{Text: "hello", Source: SourceRef{Kind: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)
	history := rt.events.History(EventFilter{RunID: runID})
	if len(history) < 3 {
		t.Fatalf("history = %#v", history)
	}
	for i, event := range history {
		if event.Version != ProtocolVersion || event.Sequence == 0 {
			t.Fatalf("event %d = %+v", i, event)
		}
		if i > 0 && event.Sequence <= history[i-1].Sequence {
			t.Fatalf("sequence not increasing: %#v", history)
		}
	}
	after := history[len(history)-2].Sequence
	filtered := rt.events.History(EventFilter{RunID: runID, After: after})
	if len(filtered) != 1 || filtered[0].Sequence <= after {
		t.Fatalf("cursor history = %#v", filtered)
	}
}

func TestDisplayMessageJSONUsesProtocolFieldNames(t *testing.T) {
	data, err := json.Marshal(DisplayMessage{
		Role: "assistant", Content: "done", Reasoning: "checked",
		Blocks: []DisplayBlock{{
			ToolName: "shell", Args: `{"command":"go test"}`,
			Content: "ok", IsError: true,
		}},
		Images: []ImageRef{{
			Path: "/tmp/image.png", URL: "https://example.test/image.png",
			Width: 640, Height: 480,
		}},
	})
	if err != nil {
		t.Fatalf("marshal display message: %v", err)
	}
	got := string(data)
	for _, want := range []string{`"role"`, `"content"`, `"reasoning"`, `"blocks"`, `"toolName"`, `"isError"`, `"images"`, `"path"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("display message json missing %s: %s", want, got)
		}
	}
	for _, unwanted := range []string{`"Role"`, `"Content"`, `"Blocks"`, `"ToolName"`, `"IsError"`, `"Images"`, `"Path"`} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("display message json leaked Go field %s: %s", unwanted, got)
		}
	}
}
