package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"nekocode/logger"
)

// sdkLogger adapts the Lark SDK's logger to the project debug channel so SDK
// diagnostics never reach stdout/stderr (they would corrupt the TUI). It
// self-filters: only Warn/Error are forwarded; the SDK's per-request and
// ping/pong Info/Debug chatter is dropped.
type sdkLogger struct{}

func (sdkLogger) Debug(context.Context, ...interface{}) {}
func (sdkLogger) Info(context.Context, ...interface{})  {}

func (sdkLogger) Warn(_ context.Context, args ...interface{}) {
	logger.Log("feishu sdk [warn] %s", strings.TrimSpace(fmt.Sprintln(args...)))
}

func (sdkLogger) Error(_ context.Context, args ...interface{}) {
	logger.Log("feishu sdk [error] %s", strings.TrimSpace(fmt.Sprintln(args...)))
}

// feishuClient wraps the Lark SDK: REST for outbound messages, websocket
// long connection for inbound events (no public URL required; the SDK
// manages tenant_access_token and auto-reconnect).
type feishuClient struct {
	appID     string
	appSecret string
	api       *lark.Client
}

func newFeishuClient(appID, appSecret string) *feishuClient {
	return &feishuClient{
		appID:     appID,
		appSecret: appSecret,
		// LogLevelError wraps the custom logger in a level-filtering proxy,
		// which also suppresses the "client ready" Info line emitted at the
		// end of NewClient.
		api: lark.NewClient(appID, appSecret,
			lark.WithLogLevel(larkcore.LogLevelError),
			lark.WithLogger(sdkLogger{}),
		),
	}
}

// sendText delivers a plain-text message to a chat (p2p chat_id of the
// paired owner in the MVP).
func (c *feishuClient) sendText(ctx context.Context, chatID, text string) error {
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	return c.create(ctx, chatID, "text", string(content))
}

// sendCard delivers an interactive card to a chat.
func (c *feishuClient) sendCard(ctx context.Context, chatID string, card map[string]any) error {
	content, err := json.Marshal(card)
	if err != nil {
		return err
	}
	return c.create(ctx, chatID, "interactive", string(content))
}

func (c *feishuClient) create(ctx context.Context, chatID, msgType, content string) error {
	resp, err := c.api.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu send message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// sendImage uploads a local image file and delivers it as an image message.
func (c *feishuClient) sendImage(ctx context.Context, chatID, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	resp, err := c.api.Im.Image.Create(ctx, larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().
			ImageType("message").
			Image(f).
			Build()).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu upload image failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.ImageKey == nil {
		return fmt.Errorf("feishu upload image returned no image_key")
	}
	content, err := json.Marshal(map[string]string{"image_key": *resp.Data.ImageKey})
	if err != nil {
		return err
	}
	return c.create(ctx, chatID, "image", string(content))
}

// feishuFileType maps a file extension to feishu's closed file-type set;
// everything outside it uploads as a generic stream.
func feishuFileType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".opus":
		return "opus"
	case ".mp4":
		return "mp4"
	case ".pdf":
		return "pdf"
	case ".doc", ".docx":
		return "doc"
	case ".xls", ".xlsx":
		return "xls"
	case ".ppt", ".pptx":
		return "ppt"
	}
	return "stream"
}

// sendFile uploads a local file and delivers it as a file message.
func (c *feishuClient) sendFile(ctx context.Context, chatID, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	resp, err := c.api.Im.File.Create(ctx, larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType(feishuFileType(path)).
			FileName(filepath.Base(path)).
			File(f).
			Build()).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu upload file failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.FileKey == nil {
		return fmt.Errorf("feishu upload file returned no file_key")
	}
	content, err := json.Marshal(map[string]string{"file_key": *resp.Data.FileKey})
	if err != nil {
		return err
	}
	return c.create(ctx, chatID, "file", string(content))
}

// downloadImage fetches an inbound message image to dst via the
// message-resource API.
func (c *feishuClient) downloadImage(ctx context.Context, messageID, imageKey, dst string) error {
	resp, err := c.api.Im.MessageResource.Get(ctx, larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(imageKey).
		Type("image").
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu download image failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return resp.WriteFile(dst)
}

// serveWS subscribes to im.message.receive_v1 and card.action.trigger over
// the SDK websocket and blocks until ctx is done or the connection fails.
func (c *feishuClient) serveWS(ctx context.Context, handler func(context.Context, *larkim.P2MessageReceiveV1) error, cardHandler func(context.Context, *larkcallback.CardActionTriggerEvent) (*larkcallback.CardActionTriggerResponse, error)) error {
	var dispatcher *larkevent.EventDispatcher
	// NewEventDispatcher hardcodes a stdout logger and emits
	// "event-dispatch is ready" from its constructor — the SDK offers no
	// injection point for that line, so it is swallowed here (see below).
	silenceStdoutAround(func() {
		dispatcher = larkevent.NewEventDispatcher("", "")
	})
	// All subsequent dispatch-path logs go to the debug channel instead.
	dispatcher.Config.Logger = sdkLogger{}
	dispatcher.OnP2MessageReceiveV1(handler)
	if cardHandler != nil {
		dispatcher.OnP2CardActionTrigger(cardHandler)
	}
	ws := larkws.NewClient(c.appID, c.appSecret,
		larkws.WithEventHandler(dispatcher),
		larkws.WithLogger(sdkLogger{}),
	)
	return ws.Start(ctx)
}

// silenceStdoutAround redirects os.Stdout to a drained pipe while fn runs.
// This is a process-global side effect and therefore limited to the one
// SDK constructor that cannot be silenced any other way; the window is a
// single function call and the pipe is drained so a concurrent writer
// (e.g. a TUI frame) cannot block.
func silenceStdoutAround(fn func()) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		fn()
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	os.Stdout = w
	fn()
	os.Stdout = orig
	_ = w.Close()
	<-done
	_ = r.Close()
}

// messageText extracts the text body of an inbound message event. ok is
// false for non-text message types or missing fields.
func messageText(ev *larkim.P2MessageReceiveV1) (chatID, openID, msgType, text string, ok bool) {
	if ev == nil || ev.Event == nil || ev.Event.Message == nil || ev.Event.Sender == nil || ev.Event.Sender.SenderId == nil {
		return "", "", "", "", false
	}
	msg := ev.Event.Message
	chatID = strVal(msg.ChatId)
	msgType = strVal(msg.MessageType)
	openID = strVal(ev.Event.Sender.SenderId.OpenId)
	if chatID == "" || openID == "" {
		return "", "", "", "", false
	}
	if msgType != "text" {
		return chatID, openID, msgType, "", true
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(strVal(msg.Content)), &body); err != nil {
		return chatID, openID, msgType, "", true
	}
	return chatID, openID, msgType, body.Text, true
}

// messageImage extracts the message_id and image_key of an inbound image
// message event.
func messageImage(ev *larkim.P2MessageReceiveV1) (messageID, imageKey string, ok bool) {
	if ev == nil || ev.Event == nil || ev.Event.Message == nil {
		return "", "", false
	}
	msg := ev.Event.Message
	var body struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(strVal(msg.Content)), &body); err != nil || body.ImageKey == "" {
		return "", "", false
	}
	return strVal(msg.MessageId), body.ImageKey, true
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
