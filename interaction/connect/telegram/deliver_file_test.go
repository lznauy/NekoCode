package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestDeliverFileSendsDocuments covers protocol.FileDeliverer on telegram:
// a running connector with paired chats must deliver the file via
// sendDocument to every paired chat.
func TestDeliverFileSendsDocuments(t *testing.T) {
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
	var documents []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendDocument") {
			// Multipart body: record the filename field of the document part.
			data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if match := multipartFilename(string(data)); match != "" {
				mu.Lock()
				documents = append(documents, match)
				mu.Unlock()
			}
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()

	client := newAPIClient("token")
	client.base = server.URL

	c := New(nil)
	c.mu.Lock()
	c.api = client
	c.mu.Unlock()

	// sendDocument uploads the local file — provide one.
	path := filepath.Join(t.TempDir(), "report.zip")
	if err := os.WriteFile(path, []byte("zip-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.DeliverFile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 || documents[0] != "report.zip" {
		t.Fatalf("documents = %v, want report.zip to the paired chat", documents)
	}

	// A stopped connector reports clearly instead of panicking.
	c.mu.Lock()
	c.api = nil
	c.mu.Unlock()
	if err := c.DeliverFile(context.Background(), "/tmp/x.zip"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("stopped connector err = %v", err)
	}
}

func multipartFilename(body string) string {
	// The filename follows 'filename="' inside a Content-Disposition line.
	idx := strings.Index(body, `filename="`)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(`filename="`):]
	if end := strings.Index(rest, `"`); end >= 0 {
		return rest[:end]
	}
	return ""
}
