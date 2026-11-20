package wecom

import (
	"context"
	"errors"
	"strings"
	"sync"
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
	c.routes["invalid-run"] = "owner"
	err := sink.Post(t.Context(), connect.Intent{Kind: connect.IntentResult, RunID: "invalid-run", Text: "invalid UTF-8: \xff"})
	if err == nil {
		t.Fatal("invalid UTF-8 was treated as deferred delivery")
	}
	if len(c.pending) != 1 || len(c.routes) != 0 {
		t.Fatalf("invalid message retained delivery state: pending=%d routes=%v", len(c.pending), c.routes)
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
	c.routes["wecom-run"] = "group-1"
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
	c.routes["run-1"] = "old-chat"
	c.enqueue(outboundMessage{chatID: "old-chat", parts: []string{"secret result"}})
	c.acceptMessage("msg-1", time.Now())
	c.clearDeliveryState()
	if len(c.routes) != 0 || len(c.pending) != 0 || len(c.seen) != 0 {
		t.Fatalf("delivery state not cleared: routes=%v pending=%v seen=%v", c.routes, c.pending, c.seen)
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

type unavailableEventsRuntime struct {
	statusRecorder
	attempts int
}

func (r *unavailableEventsRuntime) Events(context.Context, controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	r.attempts++
	return nil, errors.New("subscription offline")
}
func TestStartRetriesFailedEventSubscription(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{BotID: "bot", Secret: "secret"}); err != nil {
		t.Fatal(err)
	}
	rt := &unavailableEventsRuntime{}
	c := New(rt)
	for i := 1; i <= 2; i++ {
		if err := c.Start(t.Context()); err == nil || c.base.IsRunning() || rt.attempts != i {
			t.Fatalf("start #%d: err=%v running=%v subscriptions=%d", i, err, c.base.IsRunning(), rt.attempts)
		}
	}
}

type routedRuntime struct{ statusRecorder }

func (r *routedRuntime) StartRun(_ context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	return controlruntime.RunID(input.Text), nil
}

func TestRoutedGroupTurnsDeliverBothResults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{}
	cfg.finishPairing("owner", "group")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	c := New(&routedRuntime{})
	var wg sync.WaitGroup
	for _, id := range []string{"run_a", "queued_b", "empty"} {
		wg.Go(func() {
			if err := c.startRunRouted(t.Context(), "group", controlruntime.Input{Text: id}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	sender := &fakeSender{}
	sink := eventSink{c: c, sender: sender}
	sink.Track(controlruntime.Event{RunID: "empty", Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{}})
	for _, id := range []string{"queued_b", "run_a"} {
		if err := sink.Post(t.Context(), connect.Intent{Kind: connect.IntentResult, RunID: controlruntime.RunID(id), Text: id}); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.routes) != 0 || strings.Join(sender.sent, ",") != "queued_b,run_a" {
		t.Fatalf("routes=%v delivered=%v", c.routes, sender.sent)
	}
}

type cancelledQueueRuntime struct {
	routedRuntime
	settled chan struct{}
	checked chan struct{}
}

func (r *cancelledQueueRuntime) WaitRun(ctx context.Context, _ controlruntime.RunID) error {
	select {
	case <-r.settled:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *cancelledQueueRuntime) LookupRun(controlruntime.RunID) (controlruntime.RunSnapshot, bool) {
	close(r.checked)
	return controlruntime.RunSnapshot{}, false
}
func TestNeverStartedQueuedTurnReleasesRoute(t *testing.T) {
	rt := &cancelledQueueRuntime{settled: make(chan struct{}), checked: make(chan struct{})}
	c := New(rt)
	if err := c.startRunRouted(t.Context(), "group", controlruntime.Input{Text: "queued_test"}); err != nil {
		t.Fatal(err)
	}
	if c.routeFor("queued_test", nil) != "group" {
		t.Fatal("route missing during admission")
	}
	close(rt.settled)
	select {
	case <-rt.checked:
	case <-time.After(time.Second):
		t.Fatal("missing settlement check")
	}
	deadline := time.Now().Add(time.Second)
	for c.routeFor("queued_test", nil) != "" {
		if time.Now().After(deadline) {
			t.Fatal("cancelled queued turn retained its route")
		}
		time.Sleep(time.Millisecond)
	}
}
