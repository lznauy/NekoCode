package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	defaultWSURL = "wss://openws.work.weixin.qq.com"
	cmdSubscribe = "aibot_subscribe"
	cmdHeartbeat = "ping"
	cmdCallback  = "aibot_msg_callback"
	cmdEvent     = "aibot_event_callback"
	cmdRespond   = "aibot_respond_msg"
	cmdSend      = "aibot_send_msg"
	maxTextBytes = 20480
	writeTimeout = 10 * time.Second
)

var errSuperseded = errors.New("wecom connection superseded by another client")

type frameHeaders struct {
	RequestID string `json:"req_id"`
}

type wsFrame struct {
	Command  string          `json:"cmd,omitempty"`
	Headers  frameHeaders    `json:"headers"`
	Body     json.RawMessage `json:"body,omitempty"`
	ErrCode  *int            `json:"errcode,omitempty"`
	ErrorMsg string          `json:"errmsg,omitempty"`
}

type callbackMessage struct {
	MessageID string `json:"msgid"`
	BotID     string `json:"aibotid"`
	ChatID    string `json:"chatid,omitempty"`
	ChatType  string `json:"chattype"`
	From      struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MessageType string `json:"msgtype"`
	Text        struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Voice struct {
		Content string `json:"content"`
	} `json:"voice,omitempty"`
	Image struct {
		// URL is a short-lived download link for the image bytes.
		URL string `json:"url"`
	} `json:"image,omitempty"`
}

func (m callbackMessage) chatTarget() string {
	if m.ChatType == "group" && m.ChatID != "" {
		return m.ChatID
	}
	return m.From.UserID
}

type eventMessage struct {
	Event struct {
		EventType string `json:"eventtype"`
	} `json:"event"`
}

type messageSender interface {
	replyText(context.Context, string, string) error
	sendMarkdown(context.Context, string, string) error
}

type wsClient struct {
	botID  string
	secret string
	wsURL  string

	mu    sync.Mutex
	conn  *websocket.Conn
	ready bool
	wait  map[string]chan wsFrame
}

func newWSClient(botID, secret string) *wsClient {
	return &wsClient{botID: botID, secret: secret, wsURL: defaultWSURL, wait: make(map[string]chan wsFrame)}
}

// serve owns one authenticated WebSocket session. Reconnection is handled by
// the connector so lifecycle status remains in one place.
func (c *wsClient) serve(ctx context.Context, onReady func(), onMessage func(context.Context, wsFrame, callbackMessage)) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL, nil)
	if err != nil {
		return err
	}
	c.setConn(conn)
	defer c.clearConn(conn)

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	authID := requestID(cmdSubscribe)
	if err := c.write(ctx, wsFrame{
		Command: cmdSubscribe,
		Headers: frameHeaders{RequestID: authID},
		Body: mustJSON(map[string]string{
			"bot_id": c.botID,
			"secret": c.secret,
		}),
	}); err != nil {
		return err
	}

	ready := make(chan struct{})
	go c.heartbeat(ctx, done, ready, conn)
	authenticated := false
	for {
		// Heartbeat acknowledgements arrive as ordinary JSON frames. Requiring
		// one within this window detects half-open connections even when the OS
		// still accepts writes into a dead socket buffer.
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		var incoming wsFrame
		if err := conn.ReadJSON(&incoming); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch incoming.Command {
		case cmdCallback:
			if !authenticated {
				continue
			}
			var msg callbackMessage
			if err := json.Unmarshal(incoming.Body, &msg); err == nil {
				onMessage(ctx, incoming, msg)
			}
		case cmdEvent:
			var event eventMessage
			if json.Unmarshal(incoming.Body, &event) == nil && event.Event.EventType == "disconnected_event" {
				return errSuperseded
			}
		default:
			if incoming.Headers.RequestID == authID {
				if incoming.ErrCode == nil || *incoming.ErrCode != 0 {
					return fmt.Errorf("wecom authentication failed: %s (code %s)", incoming.ErrorMsg, errorCode(incoming.ErrCode))
				}
				if !authenticated {
					authenticated = true
					c.markReady(conn)
					close(ready)
					onReady()
				}
				continue
			}
			c.deliverAck(incoming)
		}
	}
}

func (c *wsClient) heartbeat(ctx context.Context, done, ready <-chan struct{}, conn *websocket.Conn) {
	select {
	case <-ctx.Done():
		return
	case <-done:
		return
	case <-ready:
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := c.write(ctx, wsFrame{Command: cmdHeartbeat, Headers: frameHeaders{RequestID: requestID(cmdHeartbeat)}}); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func (c *wsClient) replyText(ctx context.Context, reqID, text string) error {
	body := map[string]any{
		"msgtype": "stream",
		"stream": map[string]any{
			"id":      requestID("stream"),
			"finish":  true,
			"content": truncateUTF8(text, maxTextBytes),
		},
	}
	return c.write(ctx, wsFrame{Command: cmdRespond, Headers: frameHeaders{RequestID: reqID}, Body: mustJSON(body)})
}

func (c *wsClient) sendMarkdown(ctx context.Context, chatID, text string) error {
	body := map[string]any{
		"chatid":  chatID,
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": truncateUTF8(text, maxTextBytes),
		},
	}
	reqID := requestID(cmdSend)
	ack := make(chan wsFrame, 1)
	if err := c.writeRequest(ctx, wsFrame{Command: cmdSend, Headers: frameHeaders{RequestID: reqID}, Body: mustJSON(body)}, ack); err != nil {
		return err
	}
	timer := time.NewTimer(writeTimeout)
	defer timer.Stop()
	defer c.removeWaiter(reqID)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("wecom send acknowledgement timed out")
	case response := <-ack:
		if response.ErrCode == nil || *response.ErrCode != 0 {
			return fmt.Errorf("wecom send rejected: %s (code %s)", response.ErrorMsg, errorCode(response.ErrCode))
		}
		return nil
	}
}

func (c *wsClient) write(ctx context.Context, frame wsFrame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.conn == nil {
		return errors.New("wecom websocket is not connected")
	}
	deadline := time.Now().Add(writeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return c.conn.WriteJSON(frame)
}

func (c *wsClient) setConn(conn *websocket.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.ready = false
	c.mu.Unlock()
}

func (c *wsClient) markReady(conn *websocket.Conn) {
	c.mu.Lock()
	if c.conn == conn {
		c.ready = true
	}
	c.mu.Unlock()
}

func (c *wsClient) clearConn(conn *websocket.Conn) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
		c.ready = false
	}
	for reqID, waiter := range c.wait {
		delete(c.wait, reqID)
		close(waiter)
	}
	c.mu.Unlock()
	_ = conn.Close()
}

func (c *wsClient) writeRequest(ctx context.Context, frame wsFrame, ack chan wsFrame) error {
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return err
	}
	if c.conn == nil || !c.ready {
		c.mu.Unlock()
		return errors.New("wecom websocket is not authenticated")
	}
	c.wait[frame.Headers.RequestID] = ack
	deadline := time.Now().Add(writeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		delete(c.wait, frame.Headers.RequestID)
		c.mu.Unlock()
		return err
	}
	err := c.conn.WriteJSON(frame)
	if err != nil {
		delete(c.wait, frame.Headers.RequestID)
	}
	c.mu.Unlock()
	return err
}

func (c *wsClient) deliverAck(frame wsFrame) {
	c.mu.Lock()
	waiter := c.wait[frame.Headers.RequestID]
	if waiter != nil {
		delete(c.wait, frame.Headers.RequestID)
		waiter <- frame
	}
	c.mu.Unlock()
}

func (c *wsClient) removeWaiter(reqID string) {
	c.mu.Lock()
	delete(c.wait, reqID)
	c.mu.Unlock()
}

func requestID(prefix string) string { return prefix + "_" + uuid.NewString() }

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func errorCode(code *int) string {
	if code == nil {
		return "missing"
	}
	return fmt.Sprint(*code)
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
