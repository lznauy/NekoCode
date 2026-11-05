package wecom

import (
	"context"
	"errors"
	controlruntime "nekocode/runtime"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

type rejectImageRuntime struct {
	stubRunRuntime
	path string
}

func (s *rejectImageRuntime) StartRun(_ context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	s.path = input.Images[0].Path
	return "", errors.New("runtime busy")
}
func TestRejectedImageReleasesDownload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{}
	cfg.finishPairing("owner", "owner")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte{0xff, 0xd8, 0xff, 0xe0}) }))
	defer server.Close()
	rt := &rejectImageRuntime{}
	c := New(rt)
	msg := callbackMessage{MessageID: "rejected-image", ChatType: "single", MessageType: "image"}
	msg.From.UserID = "owner"
	msg.Image.URL = server.URL
	c.handleMessage(t.Context(), &fakeSender{}, wsFrame{Headers: frameHeaders{RequestID: "rejected"}}, msg)
	if rt.path == "" {
		t.Fatal("download did not reach runtime")
	}
	defer os.Remove(rt.path)
	if _, err := os.Stat(rt.path); !os.IsNotExist(err) {
		t.Fatalf("rejected download retained: %v", err)
	}
}
