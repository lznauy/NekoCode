package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"nekocode/protocol"
	controlruntime "nekocode/runtime"
)

// photoStubRuntime embeds the interface and overrides just what the inbound
// message path needs; unhandled methods would panic the test.
type photoStubRuntime struct {
	controlruntime.ConnectorRuntime
	started []controlruntime.Input
}

func (s *photoStubRuntime) CommandMenu(context.Context, string) (protocol.CommandMenu, bool) {
	return protocol.CommandMenu{}, false
}

func (s *photoStubRuntime) ExecuteLocalCommand(context.Context, string) (string, controlruntime.LocalCommandResult) {
	return "", controlruntime.LocalCommandNotCommand
}

func (s *photoStubRuntime) StartRun(_ context.Context, in controlruntime.Input) (controlruntime.RunID, error) {
	s.started = append(s.started, in)
	return controlruntime.RunID("run_1"), nil
}

func (s *photoStubRuntime) ReportConnectorStatus(controlruntime.ConnectorStatusPayload) {}

func (s *photoStubRuntime) CancelRun(context.Context, controlruntime.RunID) error { return nil }

func (s *photoStubRuntime) DecideApproval(context.Context, string, controlruntime.ApprovalDecision) error {
	return nil
}

func (s *photoStubRuntime) AnswerQuestion(context.Context, string, controlruntime.QuestionReply) error {
	return nil
}

func (s *photoStubRuntime) Events(context.Context, controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	return nil, nil
}

// TestHandleUpdatePhotoDownloadsAndAttaches verifies the inbound photo path:
// the largest photo size is downloaded via getFile, the text carries the
// [Image #N] marker required by InputWithImageAttachments, and the local
// path is attached to the run input.
func TestHandleUpdatePhotoDownloadsAndAttaches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{
		ActiveProfile: "personal",
		Profiles: []BotProfile{{
			Name:  "personal",
			Owner: &Device{UserID: 1, Username: "alice", ChatID: 42},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x01, 0x02}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/file_1.jpg"}}`))
		case strings.HasPrefix(r.URL.Path, "/file/bottoken/"):
			_, _ = w.Write(jpegBytes)
		default:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	defer server.Close()

	client := newAPIClient("token")
	client.base = server.URL

	rt := &photoStubRuntime{}
	conn := New(rt)
	conn.handleUpdate(context.Background(), client, cfgOwner(t), Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 7,
			From:      &User{ID: 1, Username: "alice", FirstName: "Alice"},
			Chat:      Chat{ID: 42},
			Caption:   "这是什么",
			Photo: []PhotoSize{
				{FileID: "small", Width: 80, Height: 80},
				{FileID: "large", Width: 1280, Height: 720},
			},
		},
	})

	if len(rt.started) != 1 {
		t.Fatalf("runs started = %d, want 1", len(rt.started))
	}
	in := rt.started[0]
	if in.Text != "这是什么\n[Image #7]" {
		t.Fatalf("input text = %q, want caption + image marker", in.Text)
	}
	if len(in.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(in.Images))
	}
	if in.Images[0].Label != "[Image #7]" {
		t.Fatalf("image label = %q", in.Images[0].Label)
	}
	data, err := os.ReadFile(in.Images[0].Path)
	if err != nil {
		t.Fatalf("downloaded image unreadable: %v", err)
	}
	if string(data) != string(jpegBytes) {
		t.Fatalf("downloaded image content mismatch")
	}
}

// cfgOwner reloads the saved config so handleUpdate mutates the same
// profile instance the test fixture persisted.
func cfgOwner(t *testing.T) *BotProfile {
	t.Helper()
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	profile, ok := cfg.activeProfile()
	if !ok {
		t.Fatal("no active profile")
	}
	return &profile
}

// TestHandleUpdatePhotoWithoutCaption verifies a bare photo (no caption)
// still starts a run with the marker as the sole text.
func TestHandleUpdatePhotoWithoutCaption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{
		ActiveProfile: "personal",
		Profiles: []BotProfile{{
			Name:  "personal",
			Owner: &Device{UserID: 1, Username: "alice", ChatID: 42},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/file_2.jpg"}}`))
		case strings.HasPrefix(r.URL.Path, "/file/bottoken/"):
			_, _ = w.Write([]byte{0xFF, 0xD8})
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	defer server.Close()

	client := newAPIClient("token")
	client.base = server.URL

	rt := &photoStubRuntime{}
	conn := New(rt)
	conn.handleUpdate(context.Background(), client, cfgOwner(t), Update{
		UpdateID: 2,
		Message: &Message{
			MessageID: 9,
			From:      &User{ID: 1, Username: "alice"},
			Chat:      Chat{ID: 42},
			Photo:     []PhotoSize{{FileID: "only", Width: 100, Height: 100}},
		},
	})

	if len(rt.started) != 1 {
		t.Fatalf("runs started = %d, want 1", len(rt.started))
	}
	in := rt.started[0]
	if in.Text != "[Image #9]" || len(in.Images) != 1 {
		t.Fatalf("input = %q images=%d, want marker-only text + 1 image", in.Text, len(in.Images))
	}
}

// TestLargestPhoto verifies the size selection helper.
func TestLargestPhoto(t *testing.T) {
	photo := []PhotoSize{
		{FileID: "a", Width: 100, Height: 50},
		{FileID: "b", Width: 1600, Height: 900},
		{FileID: "c", Width: 300, Height: 300},
	}
	got, ok := largestPhoto(photo)
	if !ok || got.FileID != "b" {
		t.Fatalf("largestPhoto = %q ok=%v, want b", got.FileID, ok)
	}
	if _, ok := largestPhoto(nil); ok {
		t.Fatal("empty photo array should not yield a size")
	}
}
