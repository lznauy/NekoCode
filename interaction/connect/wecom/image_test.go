package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"nekocode/protocol"
	controlruntime "nekocode/runtime"
)

// stubRunRuntime records StartRun inputs; the rest of the runtime surface is
// not exercised by the image path.
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
// path: the CDN URL is downloaded and referenced via the marker convention.
func TestHandleMessageImageDownloadsAndAttaches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{}
	cfg.finishPairing("user-1", "user-1")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x03}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jpegBytes)
	}))
	defer server.Close()

	rt := &stubRunRuntime{}
	c := New(rt)
	sender := &fakeSender{}
	msg := callbackMessage{MessageID: "m-1", ChatType: "single"}
	msg.From.UserID = "user-1"
	msg.MessageType = "image"
	msg.Image.URL = server.URL
	c.handleMessage(context.Background(), sender, wsFrame{Headers: frameHeaders{RequestID: "req-1"}}, msg)

	if len(rt.started) != 1 {
		t.Fatalf("runs started = %d, want 1", len(rt.started))
	}
	in := rt.started[0]
	if in.Text != "[Image #m-1]" {
		t.Fatalf("input text = %q, want marker", in.Text)
	}
	if len(in.Images) != 1 || in.Images[0].Label != "[Image #m-1]" {
		t.Fatalf("images = %#v", in.Images)
	}
	data, err := os.ReadFile(in.Images[0].Path)
	if err != nil || string(data) != string(jpegBytes) {
		t.Fatalf("downloaded image mismatch: %v", err)
	}
}

// A failed download replies an error note instead of starting a run.
func TestHandleMessageImageDownloadFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{}
	cfg.finishPairing("user-1", "user-1")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	rt := &stubRunRuntime{}
	c := New(rt)
	sender := &fakeSender{}
	msg := callbackMessage{MessageID: "m-2", ChatType: "single"}
	msg.From.UserID = "user-1"
	msg.MessageType = "image"
	msg.Image.URL = "" // empty URL fails fast
	c.handleMessage(context.Background(), sender, wsFrame{Headers: frameHeaders{RequestID: "req-2"}}, msg)

	if len(rt.started) != 0 {
		t.Fatalf("no run should start on download failure, got %d", len(rt.started))
	}
	if !strings.Contains(sender.text, "图片下载失败") {
		t.Fatalf("reply = %q, want download-failure note", sender.text)
	}
}
