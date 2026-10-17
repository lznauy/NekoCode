package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestHandleUpdateBarePhotoDownloadFailureReplies guards the degradation
// path: a caption-less photo whose download fails must get a friendly reply
// instead of leaking the runtime's internal "empty input" rejection.
func TestHandleUpdateBarePhotoDownloadFailureReplies(t *testing.T) {
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

	var mu sync.Mutex
	var sentTexts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/file_9.jpg"}}`))
		case strings.HasPrefix(r.URL.Path, "/file/bottoken/"):
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if text, _ := body["text"].(string); text != "" {
				mu.Lock()
				sentTexts = append(sentTexts, text)
				mu.Unlock()
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		}
	}))
	defer server.Close()

	client := newAPIClient("token")
	client.base = server.URL

	rt := &photoStubRuntime{}
	c := New(rt)
	c.handleUpdate(context.Background(), client, cfgOwner(t), Update{
		UpdateID: 5,
		Message: &Message{
			MessageID: 11,
			From:      &User{ID: 1, Username: "alice"},
			Chat:      Chat{ID: 42},
			Photo:     []PhotoSize{{FileID: "f", Width: 10, Height: 10}},
			// no caption — nothing to degrade to
		},
	})

	if len(rt.started) != 0 {
		t.Fatalf("no run should start, got %d", len(rt.started))
	}
	if len(sentTexts) == 0 || !strings.Contains(sentTexts[0], "图片下载失败") {
		t.Fatalf("sent texts = %v, want download-failure note", sentTexts)
	}
}
