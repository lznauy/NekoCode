package qqbot

import (
	"context"
	"errors"
	"io"
	controlruntime "nekocode/runtime"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type rejectImageRuntime struct {
	stubRunRuntime
	paths []string
}

func (s *rejectImageRuntime) StartRun(_ context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	for _, image := range input.Images {
		s.paths = append(s.paths, image.Path)
	}
	return "", errors.New("runtime busy")
}
func TestRejectedImagesReleaseAllDownloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte{0xff, 0xd8, 0xff, 0xe0}) }))
	defer server.Close()
	rt := &rejectImageRuntime{}
	c := New(rt)
	client := newAPIClient("local", "test", false)
	client.token, client.expiresAt = "local-token", time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: reconnectTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	c.handleMessage(t.Context(), client, inboundMessage{chatKind: "c2c", chatID: "user", authorID: "user", msgID: "rejected", imageURLs: []string{server.URL, server.URL}})
	if len(rt.paths) != 2 {
		t.Fatalf("downloads=%v", rt.paths)
	}
	for _, path := range rt.paths {
		defer os.Remove(path)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("rejected download retained: %s err=%v", path, err)
		}
	}
}
