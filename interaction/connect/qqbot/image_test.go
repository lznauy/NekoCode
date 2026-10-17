package qqbot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"nekocode/protocol"
	controlruntime "nekocode/runtime"
)

// stubRunRuntime records StartRun inputs for the image path.
type stubRunRuntime struct {
	controlruntime.ConnectorRuntime
	started []controlruntime.Input
}

func (s *stubRunRuntime) CommandMenu(context.Context, string) (protocol.CommandMenu, bool) {
	return protocol.CommandMenu{}, false
}

func (s *stubRunRuntime) ExecuteLocalCommand(context.Context, string) (string, controlruntime.LocalCommandResult) {
	return "", controlruntime.LocalCommandNotCommand
}

func (s *stubRunRuntime) StartRun(_ context.Context, in controlruntime.Input) (controlruntime.RunID, error) {
	s.started = append(s.started, in)
	return controlruntime.RunID("run_1"), nil
}

// TestHandleMessageImageDownloadsAndAttaches verifies the inbound image
// path: attachment CDN URLs are downloaded and referenced via markers.
func TestHandleMessageImageDownloadsAndAttaches(t *testing.T) {
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x04}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jpegBytes)
	}))
	defer server.Close()

	rt := &stubRunRuntime{}
	c := New(rt)
	c.handleMessage(context.Background(), nil, inboundMessage{
		chatKind:  "group",
		chatID:    "G-123",
		msgID:     "msg-9",
		authorID:  "M-456",
		text:      "看图",
		imageURLs: []string{server.URL},
	})

	if len(rt.started) != 1 {
		t.Fatalf("runs started = %d, want 1", len(rt.started))
	}
	in := rt.started[0]
	want := "看图\n[Image #msg-9-0]"
	if in.Text != want {
		t.Fatalf("input text = %q, want %q", in.Text, want)
	}
	if len(in.Images) != 1 || in.Images[0].Label != "[Image #msg-9-0]" {
		t.Fatalf("images = %#v", in.Images)
	}
	data, err := os.ReadFile(in.Images[0].Path)
	if err != nil || string(data) != string(jpegBytes) {
		t.Fatalf("downloaded image mismatch: %v", err)
	}
}

// A bare image (no text) starts a run with the marker as the sole text.
func TestHandleMessageBareImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{0xFF, 0xD8})
	}))
	defer server.Close()

	rt := &stubRunRuntime{}
	c := New(rt)
	c.handleMessage(context.Background(), nil, inboundMessage{
		chatKind:  "c2c",
		chatID:    "U-789",
		msgID:     "msg-8",
		authorID:  "U-789",
		imageURLs: []string{server.URL},
	})

	if len(rt.started) != 1 {
		t.Fatalf("runs started = %d, want 1", len(rt.started))
	}
	if in := rt.started[0]; in.Text != "[Image #msg-8-0]" || len(in.Images) != 1 {
		t.Fatalf("input = %q images=%d", in.Text, len(in.Images))
	}
}

// A message whose only attachment download fails replies a failure note
// instead of silently starting a run — or dropping the message.
func TestHandleMessageImageDownloadFailure(t *testing.T) {
	rt := &stubRunRuntime{}
	c := New(rt)
	client := newAPIClient("app", "secret", true) // unreachable endpoint; reply errors are ignored
	c.handleMessage(context.Background(), client, inboundMessage{
		chatKind:  "group",
		chatID:    "G-123",
		msgID:     "msg-7",
		authorID:  "M-456",
		imageURLs: []string{"http://127.0.0.1:0/unreachable"},
	})
	if len(rt.started) != 0 {
		t.Fatalf("runs started = %d, want 0 on download failure", len(rt.started))
	}
}
