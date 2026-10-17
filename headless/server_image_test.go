package headless

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	rt "nekocode/runtime"
)

// recordingStartBackend captures the StartRun input for assertions.
type recordingStartBackend struct {
	Backend
	mu     sync.Mutex
	inputs []rt.Input
}

func (b *recordingStartBackend) StartRun(_ context.Context, input rt.Input) (rt.RunID, error) {
	b.mu.Lock()
	b.inputs = append(b.inputs, input)
	b.mu.Unlock()
	return rt.RunID("run_1"), nil
}
func (b *recordingStartBackend) CurrentSessionID() string        { return "session-test" }
func (b *recordingStartBackend) CurrentModel() rt.ModelSelection { return rt.ModelSelection{} }
func (b *recordingStartBackend) PermissionMode() string          { return "manual" }
func (b *recordingStartBackend) Events(context.Context, rt.EventFilter) (<-chan rt.Event, error) {
	return make(chan rt.Event), nil
}

// TestStartAnchorsImagePathsAtCWD covers the server-side assembly: image
// block paths anchor at the headless working directory and reach StartRun
// via Input.Images with the marker in the text.
func TestStartAnchorsImagePathsAtCWD(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := &recordingStartBackend{}
	var output bytes.Buffer
	conn := newTransport(ctx, strings.NewReader(""), &output)
	s := &server{backend: backend, conn: conn, sessionID: "session-test", cwd: "/app/workspace/chat-1"}

	input := userInput{
		id:     "u-img",
		text:   "看看\n\n[Image #1]",
		images: []rt.ImageAttachment{{Label: "[Image #1]", Path: "uploads/shot.jpg"}},
	}
	if err := s.start(ctx, input); err != nil {
		t.Fatal(err)
	}

	backend.mu.Lock()
	got := backend.inputs[0]
	backend.mu.Unlock()
	if len(got.Images) != 1 || got.Images[0].Path != "/app/workspace/chat-1/uploads/shot.jpg" {
		t.Fatalf("images = %#v", got.Images)
	}
	if !strings.Contains(got.Text, "[Image #1]") {
		t.Fatalf("text = %q", got.Text)
	}
}

// TestStartRejectsEscapingImagePath: a path outside the working directory
// must fail the turn instead of reaching StartRun.
func TestStartRejectsEscapingImagePath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := &recordingStartBackend{}
	var output bytes.Buffer
	conn := newTransport(ctx, strings.NewReader(""), &output)
	s := &server{backend: backend, conn: conn, sessionID: "session-test", cwd: "/app/workspace/chat-1"}

	input := userInput{
		id:     "u-bad",
		text:   "[Image #1]",
		images: []rt.ImageAttachment{{Label: "[Image #1]", Path: "../../secrets.png"}},
	}
	if err := s.start(ctx, input); err != nil {
		t.Fatal(err)
	}

	backend.mu.Lock()
	n := len(backend.inputs)
	backend.mu.Unlock()
	if n != 0 {
		t.Fatal("escaping image path must not start a run")
	}
}
