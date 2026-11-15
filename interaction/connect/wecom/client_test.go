package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSClientAuthenticatesAndRepliesToCallback(t *testing.T) {
	frames := make(chan wsFrame, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var auth wsFrame
		if conn.ReadJSON(&auth) != nil {
			return
		}
		frames <- auth
		zero := 0
		if conn.WriteJSON(wsFrame{Headers: auth.Headers, ErrCode: &zero, ErrorMsg: "ok"}) != nil {
			return
		}
		body := mustJSON(map[string]any{
			"msgid": "msg-1", "aibotid": "bot-1", "chattype": "single",
			"from":    map[string]string{"userid": "user-1"},
			"msgtype": "text", "text": map[string]string{"content": "hello"},
		})
		if conn.WriteJSON(wsFrame{Command: cmdCallback, Headers: frameHeaders{RequestID: "callback-1"}, Body: body}) != nil {
			return
		}
		var reply wsFrame
		if conn.ReadJSON(&reply) == nil {
			frames <- reply
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := newWSClient("bot-1", "secret-1")
	client.wsURL = "ws" + strings.TrimPrefix(server.URL, "http")
	done := make(chan error, 1)
	go func() {
		done <- client.serve(ctx, func() {}, func(ctx context.Context, frame wsFrame, msg callbackMessage) {
			if msg.Text.Content != "hello" || msg.From.UserID != "user-1" {
				t.Errorf("callback = %#v", msg)
			}
			if err := client.replyText(ctx, frame.Headers.RequestID, "world"); err != nil {
				t.Errorf("reply: %v", err)
			}
			cancel()
		})
	}()

	auth := receiveFrame(t, frames)
	if auth.Command != cmdSubscribe {
		t.Fatalf("auth command = %q", auth.Command)
	}
	var credentials map[string]string
	if err := json.Unmarshal(auth.Body, &credentials); err != nil {
		t.Fatal(err)
	}
	if credentials["bot_id"] != "bot-1" || credentials["secret"] != "secret-1" {
		t.Fatalf("credentials = %#v", credentials)
	}
	reply := receiveFrame(t, frames)
	if reply.Command != cmdRespond || reply.Headers.RequestID != "callback-1" {
		t.Fatalf("reply frame = %#v", reply)
	}
	var body struct {
		MessageType string `json:"msgtype"`
		Stream      struct {
			Content string `json:"content"`
			Finish  bool   `json:"finish"`
		} `json:"stream"`
	}
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.MessageType != "stream" || body.Stream.Content != "world" || !body.Stream.Finish {
		t.Fatalf("reply body = %#v", body)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop after cancellation")
	}
}

func TestTruncateUTF8DoesNotSplitRune(t *testing.T) {
	if got := truncateUTF8("ab猫猫", 5); got != "ab猫" {
		t.Fatalf("truncateUTF8 = %q, want %q", got, "ab猫")
	}
}

func TestSendMarkdownWaitsForServerAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var auth wsFrame
		if conn.ReadJSON(&auth) != nil {
			return
		}
		zero := 0
		if conn.WriteJSON(wsFrame{Headers: auth.Headers, ErrCode: &zero}) != nil {
			return
		}
		var send wsFrame
		if conn.ReadJSON(&send) != nil {
			return
		}
		code := 40001
		_ = conn.WriteJSON(wsFrame{Headers: send.Headers, ErrCode: &code, ErrorMsg: "invalid chat"})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newWSClient("bot-1", "secret-1")
	client.wsURL = "ws" + strings.TrimPrefix(server.URL, "http")
	result := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- client.serve(ctx, func() {
			go func() {
				result <- client.sendMarkdownPart(ctx, "bad-chat", "result")
				cancel()
			}()
		}, func(context.Context, wsFrame, callbackMessage) {})
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "invalid chat") {
			t.Fatalf("sendMarkdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sendMarkdown did not receive acknowledgement")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func TestSendMarkdownRejectsUnauthenticatedSocket(t *testing.T) {
	client := newWSClient("bot-1", "secret-1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.sendMarkdownPart(ctx, "chat-1", "result"); err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("sendMarkdown error = %v", err)
	}
}

func receiveFrame(t *testing.T, frames <-chan wsFrame) wsFrame {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for WebSocket frame")
		return wsFrame{}
	}
}
