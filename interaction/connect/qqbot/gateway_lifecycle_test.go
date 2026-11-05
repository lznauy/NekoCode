package qqbot

import (
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

type reconnectTransport func(*http.Request) (*http.Response, error)

func (f reconnectTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReconnectReclaimsConnectionWorkers(t *testing.T) {
	up := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := up.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		_ = c.WriteJSON(map[string]any{"op": 7})
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &apiClient{token: "token", expiresAt: time.Now().Add(time.Hour), http: &http.Client{Transport: reconnectTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"url":%q}`, "ws"+strings.TrimPrefix(server.URL, "http")))), Header: make(http.Header)}, nil
	})}}
	s := gatewaySession{client: client}
	before := runtime.NumGoroutine()
	for i := 0; i < 30; i++ {
		_ = s.run(ctx, nil, nil)
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+10 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	after := runtime.NumGoroutine()
	if after > before+10 {
		t.Fatalf("connection workers leaked: before=%d after=%d", before, after)
	}
}
