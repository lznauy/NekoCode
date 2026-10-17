package wecom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nekocode/interaction/connect"
	controlruntime "nekocode/runtime"
)

// Connector bridges a WeCom intelligent robot WebSocket to the control
// runtime. The platform visibility rules are supplemented by an explicit
// NekoCode owner pairing because this channel can authorize local actions.
type Connector struct {
	rt   controlruntime.ConnectorRuntime
	base *connect.Base

	questions *connect.QuestionTracker
	menus     *connect.CommandMenus

	routeMu   sync.Mutex
	routeRun  controlruntime.RunID
	routeChat string

	pendingMu sync.Mutex
	pending   []outboundMessage
	flushMu   sync.Mutex

	seenMu sync.Mutex
	seen   map[string]time.Time
}

func New(rt controlruntime.ConnectorRuntime) *Connector {
	return &Connector{
		rt:        rt,
		base:      connect.NewBase(rt, "wecom", "WeCom"),
		questions: connect.NewQuestionTracker(),
		menus:     connect.NewCommandMenus(),
		seen:      make(map[string]time.Time),
	}
}

func (c *Connector) Name() string { return "wecom" }

func (c *Connector) Start(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if !cfg.configured() {
		return errors.New("wecom bot credentials are not configured")
	}
	if c.base.IsRunning() {
		return nil
	}
	runCtx, generation := c.base.Start(ctx)
	sessionCtx, cancelSession := context.WithCancel(runCtx)
	client := newWSClient(cfg.BotID, cfg.Secret)
	c.base.PublishStatus("connecting", "WeCom connector is authenticating.")
	eventsReady := make(chan error, 1)
	go c.eventLoop(sessionCtx, client, eventsReady, cancelSession)
	select {
	case err := <-eventsReady:
		if err != nil {
			cancelSession()
			return err
		}
	case <-sessionCtx.Done():
		cancelSession()
		return sessionCtx.Err()
	}
	go c.wsLoop(sessionCtx, client, generation, cancelSession)
	return nil
}

func (c *Connector) Stop() error { return c.base.Stop() }

func (c *Connector) ConnectorStatusView() controlruntime.ConnectorView {
	cfg, err := loadConfig()
	view := connect.StatusView("wecom", c.base.IsRunning())
	if err != nil {
		view.Status = "error"
		view.Message = err.Error()
		return view
	}
	view.Configured = cfg.configured()
	if cfg.BotID != "" {
		view.Metadata["bot_id"] = cfg.BotID
	}
	if cfg.Owner != nil {
		view.Devices = []controlruntime.ConnectorDeviceView{{
			ID:       cfg.Owner.UserID,
			Display:  cfg.Owner.UserID,
			LastSeen: cfg.Owner.LastSeen,
			PairedAt: cfg.Owner.PairedAt,
		}}
	}
	if !view.Configured {
		view.Status = "unconfigured"
		view.Message = "Run /connect wecom add <bot-id> <secret> first."
	}
	return view
}

func (c *Connector) wsLoop(ctx context.Context, client *wsClient, generation int, cancelSession context.CancelFunc) {
	defer cancelSession()
	defer c.base.MarkStopped(generation)
	backoff := time.Second
	for {
		err := client.serve(ctx, func() {
			backoff = time.Second
			c.base.PublishStatus("running", "WeCom intelligent robot connected.")
			go c.flushPending(ctx, client)
		}, func(messageCtx context.Context, frame wsFrame, msg callbackMessage) {
			c.handleMessage(messageCtx, client, frame, msg)
		})
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errSuperseded) {
			c.base.PublishStatus("error", "WeCom disconnected this client because another connection used the same bot.")
			return
		}
		c.base.PublishStatus("error", fmt.Sprintf("WeCom websocket disconnected: %v", err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (c *Connector) handleMessage(ctx context.Context, sender messageSender, frame wsFrame, msg callbackMessage) {
	dedupKey := msg.MessageID
	if dedupKey != "" {
		dedupKey = msg.BotID + "\x00" + dedupKey
	}
	if !c.acceptMessage(dedupKey, time.Now()) {
		_ = sender.replyText(ctx, frame.Headers.RequestID, "消息已收到，请勿重复发送。")
		return
	}
	text := strings.TrimSpace(msg.Text.Content)
	if msg.MessageType == "voice" {
		text = strings.TrimSpace(msg.Voice.Content)
	}
	if msg.MessageType != "text" && msg.MessageType != "voice" && msg.MessageType != "image" {
		_ = sender.replyText(ctx, frame.Headers.RequestID, "暂不支持这种消息类型，请发送文本或图片消息。")
		return
	}
	if msg.MessageType != "image" && (text == "" || msg.From.UserID == "" || msg.chatTarget() == "") {
		return
	}
	if msg.MessageType == "image" && (msg.From.UserID == "" || msg.chatTarget() == "") {
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		return
	}
	if !cfg.isAllowed(msg.From.UserID) {
		if cfg.Valid(text, time.Now().Unix()) {
			cfg.finishPairing(msg.From.UserID, msg.chatTarget())
			if err := saveConfig(cfg); err == nil {
				_ = sender.replyText(ctx, frame.Headers.RequestID, "NekoCode 已连接。现在可以发送任务或命令。")
				c.base.PublishStatus("paired", "WeCom connected: "+msg.From.UserID)
			}
			return
		}
		_ = sender.replyText(ctx, frame.Headers.RequestID, "此企业微信账号尚未连接 NekoCode。请先在 NekoCode 中运行 /connect wecom pair。")
		return
	}
	cfg.touchOwner(msg.From.UserID, msg.chatTarget())
	_ = saveConfig(cfg)

	menuResult := c.menus.HandleText(ctx, c.rt, menuScope(msg), text)
	if menuResult.Handled {
		c.handleMenuResult(ctx, sender, frame.Headers.RequestID, msg, menuResult)
		return
	}
	commands := connect.CommandHandler{RT: c.rt, Questions: c.questions}
	if reply, handled := commands.Handle(ctx, text); handled {
		_ = sender.replyText(ctx, frame.Headers.RequestID, reply)
		return
	}
	if out, status := c.rt.ExecuteLocalCommand(ctx, text); status == controlruntime.LocalCommandExecuted {
		if strings.TrimSpace(out) != "" {
			_ = sender.replyText(ctx, frame.Headers.RequestID, out)
		}
		return
	}
	// Image messages: download after the pairing gate, then reference the
	// file through the shared [Image #N] marker convention.
	var images []controlruntime.ImageAttachment
	if msg.MessageType == "image" {
		path, err := connect.DownloadImageToTemp(ctx, msg.Image.URL, "nekocode-wecom-*.jpg")
		if err != nil {
			_ = sender.replyText(ctx, frame.Headers.RequestID, "图片下载失败，请重发。")
			return
		}
		marker := fmt.Sprintf("[Image #%s]", msg.MessageID)
		text = connect.AppendMarker(text, marker)
		images = append(images, controlruntime.ImageAttachment{Label: marker, Path: path})
	}
	err = c.startRunRouted(context.WithoutCancel(ctx), msg.chatTarget(), controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "wecom", ID: msg.chatTarget()},
		Sender: controlruntime.SenderRef{ID: msg.From.UserID},
		Text:   text,
		Images: images,
	})
	if err != nil {
		_ = sender.replyText(ctx, frame.Headers.RequestID, "错误: "+err.Error())
		return
	}
	_ = sender.replyText(ctx, frame.Headers.RequestID, "任务已接收，完成后会发送结果。")
}

func menuScope(msg callbackMessage) string {
	return msg.From.UserID + "\x00" + msg.chatTarget()
}

const (
	seenMessageTTL  = 10 * time.Minute
	maxSeenMessages = 1024
)

// acceptMessage suppresses callback redelivery. WeCom documents msgid as the
// event deduplication key; without this guard a reconnect can execute the same
// local task twice.
func (c *Connector) acceptMessage(messageID string, now time.Time) bool {
	if messageID == "" {
		return true
	}
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	cutoff := now.Add(-seenMessageTTL)
	for id, seenAt := range c.seen {
		if seenAt.Before(cutoff) {
			delete(c.seen, id)
		}
	}
	if _, exists := c.seen[messageID]; exists {
		return false
	}
	if len(c.seen) >= maxSeenMessages {
		var oldestID string
		var oldest time.Time
		for id, seenAt := range c.seen {
			if oldestID == "" || seenAt.Before(oldest) {
				oldestID, oldest = id, seenAt
			}
		}
		delete(c.seen, oldestID)
	}
	c.seen[messageID] = now
	return true
}

func (c *Connector) handleMenuResult(ctx context.Context, sender messageSender, reqID string, msg callbackMessage, result connect.MenuResult) {
	switch {
	case result.Prompt != nil:
		_ = sender.replyText(ctx, reqID, connect.FormatMenu(result.Prompt))
	case result.Message != "":
		_ = sender.replyText(ctx, reqID, result.Message)
	case result.Command != "":
		if out, status := c.rt.ExecuteLocalCommand(ctx, result.Command); status == controlruntime.LocalCommandExecuted {
			_ = sender.replyText(ctx, reqID, out)
			return
		}
		err := c.startRunRouted(context.WithoutCancel(ctx), msg.chatTarget(), controlruntime.Input{
			Source: controlruntime.SourceRef{Kind: "wecom", ID: msg.chatTarget()},
			Sender: controlruntime.SenderRef{ID: msg.From.UserID},
			Text:   result.Command,
		})
		if err != nil {
			_ = sender.replyText(ctx, reqID, "错误: "+err.Error())
			return
		}
	}
}

// startRunRouted makes starting a run and publishing its conversation route
// atomic with respect to the event sink. A fast runner can finish immediately;
// holding routeMu makes its delivery wait until StartRun returns the run ID.
func (c *Connector) startRunRouted(ctx context.Context, chatID string, input controlruntime.Input) error {
	c.routeMu.Lock()
	defer c.routeMu.Unlock()
	runID, err := c.rt.StartRun(ctx, input)
	if err != nil {
		return err
	}
	c.routeRun = runID
	c.routeChat = chatID
	return nil
}

func (c *Connector) routeFor(runID controlruntime.RunID, owner *connect.Owner[string]) string {
	c.routeMu.Lock()
	defer c.routeMu.Unlock()
	if runID != "" && runID == c.routeRun {
		return c.routeChat
	}
	// Direct-message owners are private and retain the existing connector
	// behavior of receiving events from other NekoCode surfaces. A group is
	// only targeted by a run explicitly started in that group.
	if owner != nil && owner.ChatID == owner.UserID {
		return owner.ChatID
	}
	return ""
}

func (c *Connector) clearRoute(runID controlruntime.RunID) {
	c.routeMu.Lock()
	if c.routeRun == runID {
		c.routeRun = ""
		c.routeChat = ""
	}
	c.routeMu.Unlock()
}

// clearDeliveryState is required when the authorization identity changes.
// Queued messages already carry a concrete chat ID and must never survive an
// unpair/reset or bot replacement.
func (c *Connector) clearDeliveryState() {
	c.routeMu.Lock()
	c.routeRun = ""
	c.routeChat = ""
	c.routeMu.Unlock()
	c.pendingMu.Lock()
	c.pending = nil
	c.pendingMu.Unlock()
	c.seenMu.Lock()
	clear(c.seen)
	c.seenMu.Unlock()
	c.questions.Clear()
	c.menus.Clear()
}

func (c *Connector) eventLoop(ctx context.Context, sender messageSender, ready chan<- error, cancelSession context.CancelFunc) {
	defer cancelSession()
	_ = connect.DispatchReady(ctx, c.rt, eventSink{c: c, sender: sender}, ready)
}

type eventSink struct {
	c      *Connector
	sender messageSender
}

type outboundMessage struct {
	chatID   string
	text     string
	runID    controlruntime.RunID
	terminal bool
}

func (eventSink) Caps() connect.Capabilities { return connect.Capabilities{} }

func (s eventSink) Post(ctx context.Context, intent connect.Intent) error {
	switch intent.Kind {
	case connect.IntentQuestion:
		if intent.Question != nil {
			s.c.questions.Add(*intent.Question)
		}
	case connect.IntentQuestionResolved:
		s.c.questions.Remove(intent.ID)
	}
	if strings.TrimSpace(intent.Text) == "" {
		return nil
	}
	cfg, err := loadConfig()
	if err != nil || cfg.Owner == nil {
		return err
	}
	chatID := s.c.routeFor(intent.RunID, cfg.Owner)
	if chatID == "" {
		return nil
	}
	terminal := intent.Kind == connect.IntentResult || intent.Kind == connect.IntentFailed || intent.Kind == connect.IntentStopped
	out := outboundMessage{chatID: chatID, text: intent.Text, runID: intent.RunID, terminal: terminal}
	err = s.sender.sendMarkdown(ctx, out.chatID, out.text)
	if err != nil {
		s.c.enqueue(out)
		s.c.base.PublishStatus("error", "WeCom send deferred until reconnect: "+err.Error())
	} else if terminal {
		s.c.clearRoute(intent.RunID)
	}
	return nil
}

const maxPendingMessages = 64

func (c *Connector) enqueue(out outboundMessage) {
	c.pendingMu.Lock()
	if len(c.pending) >= maxPendingMessages {
		copy(c.pending, c.pending[1:])
		c.pending[len(c.pending)-1] = out
	} else {
		c.pending = append(c.pending, out)
	}
	c.pendingMu.Unlock()
}

func (c *Connector) flushPending(ctx context.Context, sender messageSender) {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	for {
		c.pendingMu.Lock()
		if len(c.pending) == 0 {
			c.pendingMu.Unlock()
			return
		}
		out := c.pending[0]
		c.pending = c.pending[1:]
		c.pendingMu.Unlock()
		if err := sender.sendMarkdown(ctx, out.chatID, out.text); err != nil {
			c.pendingMu.Lock()
			if len(c.pending) >= maxPendingMessages {
				c.pending = c.pending[:maxPendingMessages-1]
			}
			c.pending = append([]outboundMessage{out}, c.pending...)
			c.pendingMu.Unlock()
			return
		}
		if out.terminal {
			c.clearRoute(out.runID)
		}
	}
}
