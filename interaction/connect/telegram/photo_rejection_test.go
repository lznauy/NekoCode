package telegram

import (
	"context"
	"errors"
	controlruntime "nekocode/runtime"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type rejectPhotoRuntime struct {
	photoStubRuntime
	path string
}

func (s *rejectPhotoRuntime) StartRun(_ context.Context, in controlruntime.Input) (controlruntime.RunID, error) {
	s.path = in.Images[0].Path
	return "", errors.New("runtime busy")
}
func TestRejectedPhotoReleasesDownload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{Profiles: []BotProfile{{Name: "x", Owner: &Device{UserID: 1, ChatID: 42}}}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/x.jpg"}}`))
		case strings.HasPrefix(r.URL.Path, "/file/"):
			w.Write([]byte{0xff, 0xd8, 0xff, 0xe0})
		default:
			w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	defer server.Close()
	client := newAPIClient("token")
	client.base = server.URL
	rt := &rejectPhotoRuntime{}
	c := New(rt)
	c.handleUpdate(context.Background(), client, cfgOwner(t), Update{Message: &Message{MessageID: 1, From: &User{ID: 1}, Chat: Chat{ID: 42}, Photo: []PhotoSize{{FileID: "x", Width: 1, Height: 1}}}})
	if rt.path == "" {
		t.Fatal("download path not captured")
	}
	defer os.Remove(rt.path)
	if _, err := os.Stat(rt.path); !os.IsNotExist(err) {
		t.Fatalf("busy run leaked connector download: %s err=%v", rt.path, err)
	}
}
