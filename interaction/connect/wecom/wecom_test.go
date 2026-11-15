package wecom

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"nekocode/interaction/connect"
	controlruntime "nekocode/runtime"
)

type fakeSender struct {
	reqID string
	text  string
	err   error
	sent  []string
}

type statusRecorder struct {
	controlruntime.ConnectorRuntime
	statuses []controlruntime.ConnectorStatusPayload
}

func (r *statusRecorder) ReportConnectorStatus(status controlruntime.ConnectorStatusPayload) {
	r.statuses = append(r.statuses, status)
}

func TestInvalidMessageDoesNotBlockQueuedDelivery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{BotID: "bot", Secret: "secret"}
	cfg.finishPairing("owner", "owner")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	rt := &statusRecorder{}
	c := New(rt)
	sender := &fakeSender{err: errors.New("offline")}
	sink := eventSink{c: c, sender: sender}
	// A valid message is queued while offline; invalid UTF-8 must not
	// become a retryable queue entry before the next valid message.
	if err := sink.Post(t.Context(), connect.Intent{Kind: connect.IntentResult, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	c.routeRun, c.routeChat = "invalid-run", "owner"
	err := sink.Post(t.Context(), connect.Intent{Kind: connect.IntentResult, RunID: "invalid-run", Text: "invalid UTF-8: \xff"})
	if err == nil {
		t.Fatal("invalid UTF-8 was treated as deferred delivery")
	}
	if len(c.pending) != 1 || c.routeRun != "" {
		t.Fatalf("invalid message retained delivery state: pending=%d route=%s", len(c.pending), c.routeRun)
	}
	lastStatus := rt.statuses[len(rt.statuses)-1]
	if lastStatus.Status != "error" || !strings.Contains(lastStatus.Message, "prepare") || strings.Contains(lastStatus.Message, "reconnect") {
		t.Fatalf("invalid UTF-8 not reported separately: %+v", lastStatus)
	}
	if err := sink.Post(t.Context(), connect.Intent{Kind: connect.IntentResult, Text: "second"}); err != nil {
		t.Fatal(err)
	}
	sender.err = nil
	for range 3 {
		c.flushPending(t.Context(), sender)
	}
	if strings.Join(sender.sent, ",") != "first,second" || len(c.pending) != 0 {
		t.Fatalf("healthy reconnect blocked: sent=%v pending=%d", sender.sent, len(c.pending))
	}
}

func TestGroupRoutesOnlyItsOwnRun(t *testing.T) {
	c := New(nil)
	groupOwner := Config{}
	groupOwner.finishPairing("user-1", "group-1")
	if got := c.routeFor("other-run", groupOwner.Owner); got != "" {
		t.Fatalf("unrelated run routed to group: %q", got)
	}
	c.routeRun = controlruntime.RunID("wecom-run")
	c.routeChat = "group-1"
	if got := c.routeFor("wecom-run", groupOwner.Owner); got != "group-1" {
		t.Fatalf("wecom run route = %q", got)
	}
	c.clearRoute("wecom-run")
	if got := c.routeFor("wecom-run", groupOwner.Owner); got != "" {
		t.Fatalf("cleared run route = %q", got)
	}

	directOwner := Config{}
	directOwner.finishPairing("user-1", "user-1")
	if got := c.routeFor("other-run", directOwner.Owner); got != "user-1" {
		t.Fatalf("direct owner fallback = %q", got)
	}
}

func (s *fakeSender) replyText(_ context.Context, reqID, text string) error {
	s.reqID, s.text = reqID, text
	return nil
}

func (s *fakeSender) sendMarkdownPart(_ context.Context, _ string, text string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, text)
	return nil
}

func TestPendingMessagesSurviveFailedFlush(t *testing.T) {
	c := New(nil)
	c.enqueue(outboundMessage{chatID: "chat-1", parts: []string{"first"}})
	c.enqueue(outboundMessage{chatID: "chat-1", parts: []string{"second"}})
	failing := &fakeSender{err: errors.New("offline")}
	c.flushPending(context.Background(), failing)
	if len(c.pending) != 2 || strings.Join(c.pending[0].parts, "") != "first" {
		t.Fatalf("pending after failure = %#v", c.pending)
	}
	connected := &fakeSender{}
	c.flushPending(context.Background(), connected)
	if strings.Join(connected.sent, ",") != "first,second" || len(c.pending) != 0 {
		t.Fatalf("sent=%v pending=%v", connected.sent, c.pending)
	}
}

func TestClearDeliveryStateDropsOldIdentityMessages(t *testing.T) {
	c := New(nil)
	c.routeRun = "run-1"
	c.routeChat = "old-chat"
	c.enqueue(outboundMessage{chatID: "old-chat", parts: []string{"secret result"}})
	c.acceptMessage("msg-1", time.Now())
	c.clearDeliveryState()
	if c.routeRun != "" || c.routeChat != "" || len(c.pending) != 0 || len(c.seen) != 0 {
		t.Fatalf("delivery state not cleared: route=%s/%s pending=%v seen=%v", c.routeRun, c.routeChat, c.pending, c.seen)
	}
}

func TestAcceptMessageDeduplicatesAndExpires(t *testing.T) {
	c := New(nil)
	now := time.Now()
	if !c.acceptMessage("msg-1", now) || c.acceptMessage("msg-1", now.Add(time.Second)) {
		t.Fatal("duplicate message id was accepted")
	}
	if !c.acceptMessage("msg-1", now.Add(seenMessageTTL+time.Second)) {
		t.Fatal("expired message id was not accepted")
	}
	if !c.acceptMessage("", now) || !c.acceptMessage("", now) {
		t.Fatal("messages without ids should remain processable")
	}
}

func TestMenuScopeSeparatesConversations(t *testing.T) {
	direct := callbackMessage{ChatType: "single"}
	direct.From.UserID = "user-1"
	group := callbackMessage{ChatType: "group", ChatID: "group-1"}
	group.From.UserID = "user-1"
	if menuScope(direct) == menuScope(group) {
		t.Fatal("direct and group menu scopes should differ")
	}
}

func TestHandleMessagePairsOnlyMatchingNonce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{BotID: "bot", Secret: "secret"}
	cfg.Start("pair-code", time.Now(), pairingTTL)
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	c := New(nil)
	frame := wsFrame{Headers: frameHeaders{RequestID: "req-1"}}
	msg := callbackMessage{ChatType: "group", ChatID: "chat-1", MessageType: "text"}
	msg.From.UserID = "user-1"

	denied := &fakeSender{}
	msg.Text.Content = "wrong-code"
	c.handleMessage(context.Background(), denied, frame, msg)
	if !strings.Contains(denied.text, "尚未连接") {
		t.Fatalf("denied reply = %q", denied.text)
	}

	paired := &fakeSender{}
	msg.Text.Content = "pair-code"
	c.handleMessage(context.Background(), paired, frame, msg)
	if !strings.Contains(paired.text, "已连接") || paired.reqID != "req-1" {
		t.Fatalf("paired reply = %#v", paired)
	}
	saved, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Owner == nil || saved.Owner.UserID != "user-1" || saved.Owner.ChatID != "chat-1" {
		t.Fatalf("saved owner = %#v", saved.Owner)
	}
}
