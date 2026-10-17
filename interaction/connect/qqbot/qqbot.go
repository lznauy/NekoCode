package qqbot

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"nekocode/interaction/connect"
	controlruntime "nekocode/runtime"
)

// Connector 通过腾讯官方 QQ 机器人开放平台把 QQ 桥接到控制 runtime：
// 入站消息（群 @ / C2C 私聊，平台已做触发过滤，全部受理）成为 runtime
// 输入，出站经 connect 协议层（Translate → Intent → Sink）投递到已知会话。
// QQ 不能编辑消息，能力集为空：不接收流式预览，交互走斜杠命令。
type Connector struct {
	rt   controlruntime.ConnectorRuntime
	base *connect.Base

	questions *connect.QuestionTracker
	menus     *connect.CommandMenus

	mu    sync.Mutex
	chats map[string]chatSession // 已知会话（受理入站消息时记录）
}

func New(rt controlruntime.ConnectorRuntime) *Connector {
	return &Connector{
		rt:        rt,
		base:      connect.NewBase(rt, "qqbot", "QQBot"),
		questions: connect.NewQuestionTracker(),
		menus:     connect.NewCommandMenus(),
		chats:     make(map[string]chatSession),
	}
}

func (c *Connector) Name() string { return "qqbot" }

func (c *Connector) Start(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if !cfg.configured() {
		return fmt.Errorf("qqbot app credentials are not configured")
	}
	if c.base.IsRunning() {
		return nil
	}
	// base 会脱离调用方 ctx（可能是单次 HTTP 请求）并取消上一轮运行。
	runCtx, generation := c.base.Start(ctx)
	client := newAPIClient(cfg.AppID, cfg.AppSecret, cfg.Sandbox)

	c.base.PublishStatus("running", "")
	go c.wsLoop(runCtx, client, generation)
	go c.eventLoop(runCtx, client)
	return nil
}

func (c *Connector) Stop() error {
	return c.base.Stop()
}

func (c *Connector) ConnectorStatusView() controlruntime.ConnectorView {
	cfg, err := loadConfig()
	view := connect.StatusView("qqbot", c.base.IsRunning())
	if err != nil {
		view.Status = "error"
		view.Message = err.Error()
		return view
	}
	view.Configured = cfg.configured()
	if cfg.AppID != "" {
		view.Metadata["app_id"] = cfg.AppID
	}
	view.Metadata["sandbox"] = cfg.Sandbox
	if !view.Configured {
		view.Status = "unconfigured"
		view.Message = "Run /connect qqbot add <appid> <appsecret> first."
	}
	return view
}

// wsLoop 维持 gateway 连接：断线后指数退避重连（上限 30s），ctx 取消则退出。
// session 跨重连保留 sessionID/lastSeq，优先 Resume 续传。
func (c *Connector) wsLoop(ctx context.Context, client *apiClient, generation int) {
	defer c.base.MarkStopped(generation)
	session := &gatewaySession{client: client}
	backoff := time.Second
	for {
		err := session.run(ctx, func(msg inboundMessage) {
			c.handleMessage(ctx, client, msg)
		}, func() {
			c.base.PublishStatus("running", "QQBot gateway ready.")
		})
		if ctx.Err() != nil {
			return
		}
		c.base.PublishStatus("error", fmt.Sprintf("QQBot gateway disconnected: %v", err))
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

func (c *Connector) handleMessage(ctx context.Context, client *apiClient, msg inboundMessage) {
	// 受理后记录会话与触发消息 id，出站事件据此投递被动回复。
	c.rememberChat(msg)

	text := msg.text
	if text == "" && len(msg.imageURLs) == 0 {
		return
	}
	menuResult := c.menus.HandleText(ctx, c.rt, msg.sourceID(), text)
	if menuResult.Handled {
		switch {
		case menuResult.Prompt != nil:
			c.reply(ctx, client, msg, connect.FormatMenu(menuResult.Prompt))
		case menuResult.Message != "":
			c.reply(ctx, client, msg, menuResult.Message)
		case menuResult.Command != "":
			if out, status := c.rt.ExecuteLocalCommand(ctx, menuResult.Command); status == controlruntime.LocalCommandExecuted {
				c.reply(ctx, client, msg, out)
				return
			}
			_, err := c.rt.StartRun(context.WithoutCancel(ctx), controlruntime.Input{
				Source: controlruntime.SourceRef{Kind: "qqbot", ID: msg.sourceID()},
				Sender: controlruntime.SenderRef{ID: msg.authorID}, Text: menuResult.Command,
			})
			if err != nil {
				c.reply(ctx, client, msg, "错误: "+err.Error())
			}
		}
		return
	}

	// 共享命令（/stop /help /approve /always /reject /answer /dismiss），
	// 其余文本作为消息提交给 runtime。
	cmds := connect.CommandHandler{RT: c.rt, Questions: c.questions}
	if reply, handled := cmds.Handle(ctx, text); handled {
		c.reply(ctx, client, msg, reply)
		return
	}
	if out, status := c.rt.ExecuteLocalCommand(ctx, text); status == controlruntime.LocalCommandExecuted {
		c.reply(ctx, client, msg, out)
		return
	}
	// 图片附件：下载到本地并通过共享的 [Image #N] marker 约定引用。
	// 带文本的消息在图片下载失败时保留原文本继续；纯图全部下载失败
	// 则回复失败提示，而不是静默丢弃。
	var images []controlruntime.ImageAttachment
	for i, rawURL := range msg.imageURLs {
		path, err := connect.DownloadImageToTemp(ctx, rawURL, "nekocode-qqbot-*.jpg")
		if err != nil {
			continue
		}
		marker := fmt.Sprintf("[Image #%s-%d]", msg.msgID, i)
		text = connect.AppendMarker(text, marker)
		images = append(images, controlruntime.ImageAttachment{Label: marker, Path: path})
	}
	if text == "" {
		c.reply(ctx, client, msg, "图片下载失败，请重发。")
		return
	}
	_, err := c.rt.StartRun(context.WithoutCancel(ctx), controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "qqbot", ID: msg.sourceID()},
		Sender: controlruntime.SenderRef{ID: msg.authorID},
		Text:   text,
		Images: images,
	})
	if err != nil {
		c.reply(ctx, client, msg, "错误: "+err.Error())
	}
}

// reply 回发触发会话；入站触发的回复必带触发消息 id（被动回复窗口内）。
func (c *Connector) reply(ctx context.Context, client *apiClient, msg inboundMessage, text string) {
	if text == "" {
		return
	}
	_ = sendChat(ctx, client, msg.chatKind, msg.chatID, text, msg.msgID)
}

// sendChat 按会话类型发文本；msgID 非空时作为被动回复携带。
func sendChat(ctx context.Context, client *apiClient, kind, id, text, msgID string) error {
	if kind == "group" {
		return client.sendGroupMessage(ctx, id, text, msgID)
	}
	return client.sendC2CMessage(ctx, id, text, msgID)
}

// eventLoop 通过 connect 协议层把 runtime 事件投递到已知会话。
func (c *Connector) eventLoop(ctx context.Context, client *apiClient) {
	_ = connect.Dispatch(ctx, c.rt, eventSink{c: c, client: client})
}

// eventSink 是 QQ 的 connect.Sink 实现：能力集为空（纯文本广播），
// 问题意图顺带维护待答问题跟踪，/answer 因此可以省略 question id。
type eventSink struct {
	c      *Connector
	client *apiClient
}

func (s eventSink) Caps() connect.Capabilities { return connect.Capabilities{} }

func (s eventSink) Post(ctx context.Context, in connect.Intent) error {
	switch in.Kind {
	case connect.IntentQuestion:
		if in.Question != nil {
			s.c.questions.Add(*in.Question)
		}
	case connect.IntentQuestionResolved:
		s.c.questions.Remove(in.ID)
	}
	if in.Text == "" {
		return nil
	}
	s.broadcast(ctx, in.Text)
	return nil
}

// broadcast 向所有已知会话投递文本：带被动回复窗口内的 msg_id，
// 过期则尝试主动消息；失败写进 connector 状态。
func (s eventSink) broadcast(ctx context.Context, text string) {
	now := time.Now()
	for _, chat := range s.c.knownChats() {
		msgID := chat.freshMsgID(now)
		if err := sendChat(ctx, s.client, chat.kind, chat.id, text, msgID); err != nil {
			s.c.base.PublishStatus("error", fmt.Sprintf("QQBot send to %s:%s failed: %v", chat.kind, chat.id, err))
		}
	}
}

func (c *Connector) rememberChat(msg inboundMessage) {
	c.mu.Lock()
	c.chats[msg.sourceID()] = chatSession{
		kind:  msg.chatKind,
		id:    msg.chatID,
		msgID: msg.msgID,
		msgAt: time.Now(),
	}
	c.mu.Unlock()
}

func (c *Connector) knownChats() []chatSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Collect(maps.Values(c.chats))
}
