package wecom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

func longMessageClient(t *testing.T, failSends ...int) (*wsClient, <-chan wsFrame) {
	t.Helper()
	frames := make(chan wsFrame, 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		ordinal := 0
		for {
			var frame wsFrame
			if conn.ReadJSON(&frame) != nil {
				return
			}
			frames <- frame
			if frame.Command == cmdSend {
				ordinal++
				zero := 0
				for _, failed := range failSends {
					if ordinal == failed {
						zero = 40001
					}
				}
				if conn.WriteJSON(wsFrame{Headers: frame.Headers, ErrCode: &zero}) != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client := newWSClient("bot", "secret")
	client.setConn(conn)
	client.markReady(conn)
	go func() {
		for {
			var ack wsFrame
			if conn.ReadJSON(&ack) != nil {
				return
			}
			client.deliverAck(ack)
		}
	}()
	return client, frames
}

func TestLongMessagesKeepAllContent(t *testing.T) {
	for _, reply := range []bool{false, true} {
		t.Run(map[bool]string{false: "markdown", true: "reply"}[reply], func(t *testing.T) {
			client, frames := longMessageClient(t)
			code := strings.Repeat("猫🐱 code\n", 5000)
			text := "before\n```go\n" + code + "```\nafter"
			var err error
			if reply {
				err = client.replyText(t.Context(), "callback", text)
			} else {
				parts, splitErr := splitMessage(text, maxTextBytes)
				if splitErr != nil {
					t.Fatal(splitErr)
				}
				out := outboundMessage{chatID: "chat", parts: parts}
				err = out.send(t.Context(), client)
			}
			if err != nil {
				t.Fatal(err)
			}
			var delivered strings.Builder
			streamIDs := map[string]bool{}
			count := 0
			for {
				count++
				frame := receiveFrame(t, frames)
				var body struct {
					ChatID string `json:"chatid"`
					Stream struct {
						ID      string `json:"id"`
						Content string `json:"content"`
						Finish  bool   `json:"finish"`
					}
					Markdown struct {
						Content string `json:"content"`
					}
				}
				if err := json.Unmarshal(frame.Body, &body); err != nil {
					t.Fatal(err)
				}
				part := body.Markdown.Content
				if reply {
					part = body.Stream.Content
					if frame.Headers.RequestID != "callback" || !body.Stream.Finish || streamIDs[body.Stream.ID] {
						t.Fatalf("invalid continuation stream: %+v", body.Stream)
					}
					streamIDs[body.Stream.ID] = true
				} else if body.ChatID != "chat" {
					t.Fatal("continuation lost chat")
				}
				if len(part) > maxTextBytes || !utf8.ValidString(part) {
					t.Fatalf("invalid part: bytes=%d fences=%d", len(part), strings.Count(part, "```"))
				}
				delivered.WriteString(part)
				if strings.HasSuffix(part, "after") {
					break
				}
			}
			if count < 2 {
				t.Fatalf("long message sent %d frame(s)", count)
			}
			if got := delivered.String(); got != text {
				t.Fatalf("content changed: got %d bytes, want %d", len(got), len(text))
			}
		})
	}
}

func TestMarkdownRetryResumesUnacknowledgedParts(t *testing.T) {
	client, frames := longMessageClient(t, 2, 4)
	text := "before\n```go\n" + strings.Repeat("item\n", 10000) + "```\nafter"
	parts, err := splitMessage(text, maxTextBytes)
	if err != nil || len(parts) != 3 {
		t.Fatalf("parts=%d err=%v", len(parts), err)
	}
	out := outboundMessage{chatID: "chat", parts: parts}
	if err := out.send(t.Context(), client); err == nil {
		t.Fatal("part rejection reported success")
	}
	if len(out.parts) != 2 {
		t.Fatalf("initial retry progress: %+v", out)
	}
	connector := New(nil)
	connector.enqueue(out)
	connector.flushPending(t.Context(), client)
	if len(connector.pending) != 1 || len(connector.pending[0].parts) != 1 || connector.pending[0].parts[0] != parts[2] {
		t.Fatalf("retry repeated confirmed parts: %+v", connector.pending)
	}
	connector.flushPending(t.Context(), client)
	if len(connector.pending) != 0 {
		t.Fatal("completed message remains queued")
	}
	for i, want := range []string{parts[0], parts[1], parts[1], parts[2], parts[2]} {
		frame := receiveFrame(t, frames)
		var body struct {
			Markdown struct {
				Content string `json:"content"`
			}
		}
		if err := json.Unmarshal(frame.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body.Markdown.Content != want {
			t.Fatalf("frame %d repeated an acknowledged prefix or lost a suffix", i)
		}
	}
}
