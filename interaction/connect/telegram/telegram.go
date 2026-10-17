package telegram

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekocode/interaction/connect"
	"nekocode/interaction/connect/telegram/internal/taskview"
	controlruntime "nekocode/runtime"
)

// msgRef locates a sent interactive message so it can be terminalized
// (verdict text + keyboard stripped) once its approval/question resolves.
type msgRef struct {
	chatID    int64
	messageID int
	text      string
}

type Connector struct {
	rt   controlruntime.ConnectorRuntime
	base *connect.Base

	// questions tracks pending questions (fed by the sink's question
	// intents) so /answer //dismiss and button callbacks can resolve them.
	questions *connect.QuestionTracker
	menus     *connect.CommandMenus

	mu          sync.Mutex
	active      string
	api         *apiClient
	taskTracker *taskview.Tracker
	// pendingMsgs maps approval/question IDs to their interactive messages;
	// pendingSelect holds multi-select toggle state per question ID.
	pendingMsgs   map[string]msgRef
	pendingSelect map[string]map[int]bool
}

func New(rt controlruntime.ConnectorRuntime) *Connector {
	return &Connector{
		rt:            rt,
		base:          connect.NewBase(rt, "telegram", "Telegram"),
		questions:     connect.NewQuestionTracker(),
		menus:         connect.NewCommandMenus(),
		taskTracker:   taskview.NewTracker(),
		pendingMsgs:   make(map[string]msgRef),
		pendingSelect: make(map[string]map[int]bool),
	}
}

func (c *Connector) Name() string { return "telegram" }

func (c *Connector) ConnectorStatusView() controlruntime.ConnectorView {
	cfg, err := loadConfig()
	view := connect.StatusView("telegram", c.base.IsRunning())
	if err != nil {
		view.Status = "error"
		view.Message = err.Error()
		return view
	}
	profile, ok := cfg.activeProfile()
	view.Configured = ok && strings.TrimSpace(profile.BotToken) != ""
	if cfg.ActiveProfile != "" {
		view.Metadata["active_profile"] = cfg.ActiveProfile
	}
	if profile.BotUsername != "" {
		view.Metadata["bot_username"] = profile.BotUsername
	}
	if profile.Nonce != "" && time.Now().Unix() <= profile.Expires {
		view.Metadata["pairing_expires"] = profile.Expires
	}
	if profile.Owner != nil {
		device := *profile.Owner
		view.Devices = append(view.Devices, controlruntime.ConnectorDeviceView{
			ID:       strconv.FormatInt(device.UserID, 10),
			Username: device.Username,
			LastSeen: device.LastSeen,
			PairedAt: device.PairedAt,
		})
	}
	if !view.Configured {
		view.Status = "unconfigured"
		view.Message = "Run /connect telegram add <bot-token> first."
	}
	return view
}

func (c *Connector) Start(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.activeProfile()
	if !ok || strings.TrimSpace(profile.BotToken) == "" {
		return fmt.Errorf("telegram token is not configured")
	}
	c.mu.Lock()
	if c.base.IsRunning() && c.active == profile.Name {
		c.mu.Unlock()
		return nil
	}
	// The base detaches from the caller's context (which may be a single
	// HTTP request or a finished run) and cancels any previous run.
	runCtx, generation := c.base.Start(ctx)
	client := newAPIClient(profile.BotToken)
	c.active = profile.Name
	c.api = client
	c.mu.Unlock()

	c.base.PublishStatus("running", "")
	go c.syncCommands(runCtx, client)
	go c.pollLoop(runCtx, client, generation)
	go c.dispatchLoop(runCtx, client)
	return nil
}

func (c *Connector) syncCommands(ctx context.Context, client *apiClient) {
	menu, ok := c.rt.CommandMenu(ctx, "/")
	if !ok {
		return
	}
	commands := make([]botCommand, 0, len(menu.Items))
	for _, item := range menu.Items {
		if !strings.HasPrefix(item.Value, "/") {
			continue
		}
		name := strings.TrimPrefix(item.Value, "/")
		if name == "" || strings.ContainsAny(name, " \t") {
			continue
		}
		description := item.Description
		if description == "" {
			description = item.Label
		}
		commands = append(commands, botCommand{Command: name, Description: description})
	}
	if len(commands) > 0 {
		_ = client.setCommands(ctx, commands)
	}
}

func (c *Connector) Stop() error {
	c.mu.Lock()
	c.active = ""
	c.api = nil
	c.mu.Unlock()
	return c.base.Stop()
}

func (c *Connector) pollLoop(ctx context.Context, client *apiClient, generation int) {
	defer func() {
		c.mu.Lock()
		c.active = ""
		c.api = nil
		c.mu.Unlock()
		c.base.MarkStopped(generation)
	}()
	for {
		cfg, err := loadConfig()
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		idx := cfg.activeIndex()
		if idx < 0 {
			time.Sleep(3 * time.Second)
			continue
		}
		profile := &cfg.Profiles[idx]
		updates, err := client.getUpdates(ctx, profile.UpdateOffset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(3 * time.Second)
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= profile.UpdateOffset {
				profile.UpdateOffset = update.UpdateID + 1
			}
			c.handleUpdate(ctx, client, profile, update)
		}
		_ = saveConfig(cfg)
	}
}

func (c *Connector) handleUpdate(ctx context.Context, client *apiClient, profile *BotProfile, update Update) {
	if update.CallbackQuery != nil {
		c.handleCallbackQuery(ctx, client, profile, update.CallbackQuery)
		return
	}
	if update.Message == nil || update.Message.From == nil {
		return
	}
	msg := update.Message
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		// Photo messages carry their user prompt in Caption (if any); the
		// image marker is appended later, right before StartRun.
		if len(msg.Photo) == 0 {
			return
		}
		text = strings.TrimSpace(msg.Caption)
	}
	if nonce, ok := parseStartPayload(text); ok {
		if profile.Valid(nonce, time.Now().Unix()) {
			if profile.Owner != nil && profile.Owner.UserID != msg.From.ID {
				_ = client.sendMessage(ctx, msg.Chat.ID, "Pairing failed: this Telegram profile is already paired. Run /connect telegram unpair in NekoCode first.")
				return
			}
			profile.setOwner(msg.From.ID, msg.From.Username, msg.Chat.ID)
			profile.Clear()
			_ = client.sendMessage(ctx, msg.Chat.ID, "NekoCode connected. Send a message to control the current session.")
			c.base.PublishStatus("paired", fmt.Sprintf("Telegram connected: @%s", msg.From.Username))
			return
		}
		if !profile.isAllowed(msg.From.ID) {
			_ = client.sendMessage(ctx, msg.Chat.ID, pairingFailureMessage(nonce, profile))
			return
		}
	}
	if !profile.isAllowed(msg.From.ID) {
		_ = client.sendMessage(ctx, msg.Chat.ID, "This Telegram account is not connected to NekoCode. Run /connect telegram pair in NekoCode to bind this account.")
		return
	}
	profile.touchOwner(msg.From.ID, msg.From.Username, msg.Chat.ID)
	menuResult := c.menus.HandleText(ctx, c.rt, strconv.FormatInt(msg.Chat.ID, 10), text)
	if menuResult.Handled {
		_ = c.sendMenuResult(ctx, client, msg.Chat.ID, *msg.From, menuResult)
		return
	}

	// Shared commands (/stop /help /approve /reject /answer /dismiss) come
	// from the connector core; telegram-specific commands follow below.
	cmds := connect.CommandHandler{RT: c.rt, Questions: c.questions}
	if reply, handled := cmds.Handle(ctx, text); handled {
		_ = client.sendMessage(ctx, msg.Chat.ID, taskview.HTMLEscape(reply))
		return
	}

	switch {
	case text == "/last":
		_ = client.sendMessage(ctx, msg.Chat.ID, c.tracker().LastSummary())
	case text == "/diff" || strings.HasPrefix(text, "/diff "):
		runID := strings.TrimSpace(strings.TrimPrefix(text, "/diff"))
		_ = client.sendMessage(ctx, msg.Chat.ID, c.tracker().DiffSummary(controlruntime.RunID(runID)))
	case text == "/status":
		_ = client.sendMessage(ctx, msg.Chat.ID, c.tracker().Status())
	default:
		// During-task-safe commands (e.g. /permission, /context) run without
		// a run lifecycle — including while a run is in progress.
		if out, status := c.rt.ExecuteLocalCommand(ctx, text); status == controlruntime.LocalCommandExecuted {
			if strings.TrimSpace(out) != "" {
				_ = client.sendMessage(ctx, msg.Chat.ID, taskview.HTMLEscape(out))
			}
			return
		}
		// Photo messages: download the image and reference it from the text
		// via the [Image #N] marker convention (see InputWithImageAttachments
		// — the marker must appear in Text for the attachment to be valid).
		var images []controlruntime.ImageAttachment
		if len(msg.Photo) > 0 {
			label := fmt.Sprintf("[Image #%d]", msg.MessageID)
			if path, err := savePhoto(ctx, client, msg); err == nil {
				text = connect.AppendMarker(text, label)
				images = append(images, controlruntime.ImageAttachment{Label: label, Path: path})
			} else {
				c.base.PublishStatus("warning", "photo download failed: "+err.Error())
				if text == "" {
					// A bare photo cannot degrade to text: without the image
					// there is no input at all, and StartRun would reject it
					// with the internal "empty input" error. Tell the user
					// what actually happened instead.
					_ = client.sendMessage(ctx, msg.Chat.ID, "图片下载失败，请重发。")
					return
				}
			}
		}
		_, err := c.rt.StartRun(context.WithoutCancel(ctx), controlruntime.Input{
			Source: controlruntime.SourceRef{Kind: "telegram", ID: strconv.FormatInt(msg.Chat.ID, 10)},
			Sender: controlruntime.SenderRef{
				ID:       strconv.FormatInt(msg.From.ID, 10),
				Username: msg.From.Username,
				Display:  msg.From.FirstName,
			},
			Text:   text,
			Images: images,
		})
		if err != nil {
			_ = client.sendMessage(ctx, msg.Chat.ID, taskview.HTMLEscape("Error: "+err.Error()))
		}
	}
}

// savePhoto downloads the largest resolution of a photo message to a
// temp file and returns its path. The file outlives the run — the model may
// re-read it later via the image_understand tool.
func savePhoto(ctx context.Context, client *apiClient, msg *Message) (string, error) {
	size, ok := largestPhoto(msg.Photo)
	if !ok {
		return "", fmt.Errorf("empty photo array")
	}
	file, err := client.getFile(ctx, size.FileID)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(file.FilePath))
	if ext == "" {
		ext = ".jpg"
	}
	return connect.DownloadToTemp("nekocode-telegram-*"+ext, func(dst string) error {
		return client.downloadFile(ctx, file.FilePath, dst)
	})
}

func parseStartPayload(text string) (payload string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", false
	}
	cmd := strings.ToLower(fields[0])
	if cmd != "/start" && !strings.HasPrefix(cmd, "/start@") {
		return "", false
	}
	if len(fields) == 1 {
		return "", true
	}
	return strings.TrimSpace(fields[1]), true
}

func pairingFailureMessage(nonce string, profile *BotProfile) string {
	if strings.TrimSpace(nonce) == "" {
		return "Pairing failed: the Telegram start message did not include a pairing code. Open the full link from /connect telegram, or manually send /start <code>."
	}
	if profile.Nonce == "" || time.Now().Unix() > profile.Expires {
		return "Pairing failed: the pairing link is expired. Run /connect telegram pair in NekoCode again and open the new link."
	}
	return "Pairing failed: the pairing code does not match the current NekoCode session. Run /connect telegram pair again and use the newest link."
}

func (c *Connector) handleCallbackQuery(ctx context.Context, client *apiClient, profile *BotProfile, cb *CallbackQuery) {
	if cb == nil {
		return
	}
	chatID := int64(0)
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
	}
	if !profile.isAllowed(cb.From.ID) {
		_ = client.answerCallbackQuery(ctx, cb.ID, "This Telegram account is not connected.")
		if chatID != 0 {
			_ = client.sendMessage(ctx, chatID, "This Telegram account is not connected to NekoCode.")
		}
		return
	}
	if chatID != 0 {
		profile.touchOwner(cb.From.ID, cb.From.Username, chatID)
	}
	if strings.HasPrefix(cb.Data, "cmd:") || strings.HasPrefix(cb.Data, "cmdp:") {
		result := c.menus.Select(ctx, c.rt, strconv.FormatInt(chatID, 10), cb.Data)
		callbackMessage := "已选择"
		if result.Prompt != nil && cb.Message != nil {
			text := taskview.HTMLEscape(menuPromptText(result.Prompt))
			keyboard := commandMenuKeyboard(result.Prompt)
			_ = client.editMessage(ctx, chatID, cb.Message.MessageID, text, &keyboard)
		} else {
			submitErr := c.sendMenuResult(ctx, client, chatID, cb.From, result)
			if submitErr != nil {
				callbackMessage = "提交失败"
			}
			if cb.Message != nil {
				text := result.Message
				if result.Command != "" {
					text = "已提交: " + result.Command
					if submitErr != nil {
						text = "提交失败: " + submitErr.Error()
					}
				}
				if text != "" {
					_ = client.editMessage(ctx, chatID, cb.Message.MessageID, taskview.HTMLEscape(text), &emptyKeyboard)
				}
			}
		}
		_ = client.answerCallbackQuery(ctx, cb.ID, callbackMessage)
		return
	}

	parts := strings.Split(cb.Data, ":")
	if len(parts) < 2 {
		_ = client.answerCallbackQuery(ctx, cb.ID, "未知操作。")
		return
	}
	action, id := parts[0], parts[1]

	switch action {
	case connect.ActionOnce, connect.ActionAlways, connect.ActionReject:
		verdict := connect.VerdictForAction(action)
		decision, err := connect.ApprovalDecisionFor(action)
		if err == nil {
			err = c.rt.DecideApproval(ctx, id, decision)
		}
		if err != nil {
			if connect.IsResolvedErr(err) {
				c.terminalize(ctx, client, id, verdict)
				_ = client.answerCallbackQuery(ctx, cb.ID, "该请求已处理")
				return
			}
			_ = client.answerCallbackQuery(ctx, cb.ID, "错误: "+err.Error())
			return
		}
		c.terminalize(ctx, client, id, verdict)
		_ = client.answerCallbackQuery(ctx, cb.ID, verdict)
	case connect.ActionDismiss:
		_, err := c.questions.Reject(id)
		if err == nil {
			err = c.rt.AnswerQuestion(ctx, id, controlruntime.QuestionReply{Rejected: true})
		}
		if err != nil {
			if connect.IsResolvedErr(err) {
				c.terminalize(ctx, client, id, "已忽略")
				_ = client.answerCallbackQuery(ctx, cb.ID, "该请求已处理")
				return
			}
			_ = client.answerCallbackQuery(ctx, cb.ID, "错误: "+err.Error())
			return
		}
		c.questions.Remove(id)
		c.terminalize(ctx, client, id, "已忽略")
		_ = client.answerCallbackQuery(ctx, cb.ID, "已忽略")
	case "answer":
		c.handleAnswerCallback(ctx, client, cb, id, parts[2:])
	default:
		_ = client.answerCallbackQuery(ctx, cb.ID, "未知操作。")
	}
}

func (c *Connector) sendMenuResult(ctx context.Context, client *apiClient, chatID int64, sender User, result connect.MenuResult) error {
	if result.Prompt != nil {
		text := taskview.HTMLEscape(menuPromptText(result.Prompt))
		_, err := client.sendMessageWithKeyboard(ctx, chatID, text, commandMenuKeyboard(result.Prompt))
		return err
	}
	if result.Message != "" {
		return client.sendMessage(ctx, chatID, taskview.HTMLEscape(result.Message))
	}
	if result.Command == "" {
		return nil
	}
	// Menu submissions of during-task-safe commands take the local path too —
	// otherwise picking /permission from a menu during a run fails with busy.
	if out, status := c.rt.ExecuteLocalCommand(ctx, result.Command); status == controlruntime.LocalCommandExecuted {
		if strings.TrimSpace(out) != "" {
			return client.sendMessage(ctx, chatID, taskview.HTMLEscape(out))
		}
		return nil
	}
	_, err := c.rt.StartRun(context.WithoutCancel(ctx), controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "telegram", ID: strconv.FormatInt(chatID, 10)},
		Sender: controlruntime.SenderRef{
			ID: strconv.FormatInt(sender.ID, 10), Username: sender.Username, Display: sender.FirstName,
		},
		Text: result.Command,
	})
	if err != nil {
		_ = client.sendMessage(ctx, chatID, taskview.HTMLEscape("Error: "+err.Error()))
	}
	return err
}

func commandMenuKeyboard(prompt *connect.MenuPrompt) inlineKeyboardMarkup {
	rows := make([][]inlineKeyboardButton, 0, len(prompt.Choices))
	for _, choice := range prompt.Choices {
		rows = append(rows, []inlineKeyboardButton{{Text: menuChoiceButtonText(choice), CallbackData: choice.Token}})
	}
	return inlineKeyboardMarkup{InlineKeyboard: rows}
}

// menuPromptText renders the body accompanying a menu keyboard. Unlike
// connect.FormatMenu — the fallback for button-less transports — it does not
// repeat the choices as a numbered list: the inline keyboard already lists
// them, and choice descriptions ride in the button text.
func menuPromptText(prompt *connect.MenuPrompt) string {
	title := strings.TrimSpace(prompt.Title)
	if title == "" {
		title = "Commands"
	}
	if len(prompt.Choices) > 0 {
		return title
	}
	empty := strings.TrimSpace(prompt.Empty)
	if empty == "" {
		empty = "No choices available"
	}
	return title + "\n\n" + empty
}

// menuChoiceButtonText packs label + description into one button line so the
// keyboard carries all the information the old text list did.
func menuChoiceButtonText(choice connect.MenuChoice) string {
	text := choice.Label
	if choice.Description != "" {
		text += " — " + choice.Description
	}
	return connect.TruncateRunes(text, 64)
}

// handleAnswerCallback dispatches question-answer callbacks: single-select
// submits immediately; multi-select toggles options (redrawing the keyboard)
// until the confirm button submits the selection.
func (c *Connector) handleAnswerCallback(ctx context.Context, client *apiClient, cb *CallbackQuery, id string, rest []string) {
	if len(rest) == 0 {
		_ = client.answerCallbackQuery(ctx, cb.ID, "未知操作。")
		return
	}

	// Confirm submits the accumulated multi-selection.
	if rest[0] == connect.ActionConfirm {
		c.mu.Lock()
		selected := c.pendingSelect[id]
		c.mu.Unlock()
		indices := make([]int, 0, len(selected))
		for idx, on := range selected {
			if on {
				indices = append(indices, idx)
			}
		}
		reply, resolvedID, err := c.questions.BuildMultiOptionReply(id, indices)
		if err == nil {
			err = c.rt.AnswerQuestion(ctx, resolvedID, reply)
		}
		if err != nil {
			if connect.IsResolvedErr(err) {
				c.terminalize(ctx, client, id, "已回答")
				_ = client.answerCallbackQuery(ctx, cb.ID, "该请求已处理")
				return
			}
			_ = client.answerCallbackQuery(ctx, cb.ID, "错误: "+err.Error())
			return
		}
		c.questions.Remove(resolvedID)
		c.terminalize(ctx, client, id, "已回答")
		_ = client.answerCallbackQuery(ctx, cb.ID, "已回答")
		return
	}

	idx, parseErr := strconv.Atoi(rest[0])
	if parseErr != nil {
		_ = client.answerCallbackQuery(ctx, cb.ID, "未知操作。")
		return
	}

	// Multi-select: toggle the option and redraw the keyboard in place.
	if view, ok := c.questions.View(id); ok && len(view.Questions) == 1 && view.Questions[0].Multiple {
		if idx < 0 || idx >= len(view.Questions[0].Options) {
			_ = client.answerCallbackQuery(ctx, cb.ID, "未知选项。")
			return
		}
		c.mu.Lock()
		sel := c.pendingSelect[id]
		if sel == nil {
			sel = make(map[int]bool)
			c.pendingSelect[id] = sel
		}
		sel[idx] = !sel[idx]
		ref, hasRef := c.pendingMsgs[id]
		c.mu.Unlock()
		if hasRef {
			keyboardIn := connect.Intent{ID: id, Question: &view, Actions: connect.QuestionActions(view)}
			if keyboard, ok := c.questionKeyboard(keyboardIn); ok {
				_ = client.editMessage(ctx, ref.chatID, ref.messageID, ref.text, &keyboard)
			}
		}
		_ = client.answerCallbackQuery(ctx, cb.ID, "")
		return
	}

	// Single-select: submit immediately.
	reply, resolvedID, err := c.questions.BuildOptionReply(id, idx)
	if err == nil {
		err = c.rt.AnswerQuestion(ctx, resolvedID, reply)
	}
	if err != nil {
		if connect.IsResolvedErr(err) {
			c.terminalize(ctx, client, id, "已回答")
			_ = client.answerCallbackQuery(ctx, cb.ID, "该请求已处理")
			return
		}
		_ = client.answerCallbackQuery(ctx, cb.ID, "错误: "+err.Error())
		return
	}
	c.questions.Remove(resolvedID)
	c.terminalize(ctx, client, id, "已回答")
	_ = client.answerCallbackQuery(ctx, cb.ID, "已回答")
}

// dispatchLoop 通过 connect 协议层把 runtime 事件投递到配对会话;事件过滤、
// 顺序保证与 run 卡片记账(connect.Tracker)都在 Dispatch 里,这里只挂 sink。
func (c *Connector) dispatchLoop(ctx context.Context, client *apiClient) {
	_ = connect.Dispatch(ctx, c.rt, newEventSink(c, client))
}

// eventSink 是 Telegram 的 connect.Sink 实现:能力最全(原地编辑 + 按钮),
// 持有流式预览跟踪器,交互消息记录进 pendingMsgs 以便 terminalize。
type eventSink struct {
	c       *Connector
	client  *apiClient
	preview *previewTracker
}

func newEventSink(c *Connector, client *apiClient) *eventSink {
	return &eventSink{c: c, client: client, preview: newPreviewTracker(client, c.pairedChats)}
}

func (s *eventSink) Caps() connect.Capabilities {
	return connect.Capabilities{EditMessages: true, Buttons: true, Images: true}
}

// FlushInterval/Flush 实现 connect.Flusher:Dispatch 的 ticker 以此间隔
// 推动预览的原地编辑节流。
func (s *eventSink) FlushInterval() time.Duration { return previewEditInterval / 2 }

func (s *eventSink) Flush(ctx context.Context) { s.preview.flush(ctx) }

// Track 实现 connect.Tracker:Dispatch 在同一 goroutine 里按事件顺序同步
// 记账到 run 卡片(/last /diff /status 与 DoneReply 的数据源),保证渲染
// DoneReply 时卡片已存在——另起并行订阅会与 Dispatch 竞态,结果消息会
// 因卡片缺失而被静默丢弃。
func (s *eventSink) Track(ev controlruntime.Event) { s.c.tracker().Track(ev) }

func (s *eventSink) Post(ctx context.Context, in connect.Intent) error {
	switch in.Kind {
	case connect.IntentPreview:
		s.preview.addDelta(ctx, in.RunID, in.Text)
	case connect.IntentResult:
		// Referenced local images go out as native photos first; the text
		// (which still cites the paths) follows as the run's done-reply.
		for _, path := range in.Images {
			for _, chatID := range s.c.pairedChats() {
				_ = s.client.sendPhoto(ctx, chatID, path, "")
			}
		}
		// A settled preview already shows the final text; the done-reply
		// would duplicate it.
		if s.preview.finalize(ctx, in.RunID) {
			return nil
		}
		text := s.c.tracker().DoneReply(in.RunID)
		if text == "" {
			// The tracker never saw this run (e.g. it started before the
			// connector connected) — deliver the raw result rather than
			// dropping the message entirely.
			text = taskview.MarkdownToHTML(in.Text)
		}
		s.broadcast(ctx, text)
	case connect.IntentSystem:
		// Command/system reply (e.g. /connect, /model): deliver directly,
		// bypassing the run-terminalization logic of IntentResult.
		s.broadcast(ctx, taskview.MarkdownToHTML(in.Text))
	case connect.IntentFailed:
		// Settle the preview but keep the failure card (error context).
		s.preview.finalize(ctx, in.RunID)
		if text := s.c.tracker().DoneReply(in.RunID); text != "" {
			s.broadcast(ctx, text)
		} else {
			s.broadcast(ctx, taskview.HTMLEscape(in.Text))
		}
	case connect.IntentStopped:
		s.preview.finalize(ctx, in.RunID)
		s.broadcast(ctx, taskview.StoppedMessage())
	case connect.IntentApproval:
		if in.Approval == nil {
			return nil
		}
		s.sendInteractive(ctx, in.ID, taskview.ApprovalMessage(*in.Approval), approvalKeyboard(in))
	case connect.IntentApprovalResolved:
		// Resolved on another surface (TUI, HTTP): terminalize the pending
		// message with the canonical verdict (first-answer-wins).
		s.c.terminalize(ctx, s.client, in.ID, in.Verdict)
	case connect.IntentQuestion:
		if in.Question == nil {
			return nil
		}
		s.c.questions.Add(*in.Question)
		text := taskview.QuestionMessage(*in.Question)
		if keyboard, ok := s.c.questionKeyboard(in); ok {
			s.sendInteractive(ctx, in.ID, text, keyboard)
		} else {
			s.broadcast(ctx, text)
		}
	case connect.IntentQuestionResolved:
		s.c.questions.Remove(in.ID)
		s.c.terminalize(ctx, s.client, in.ID, in.Verdict)
	}
	return nil
}

// broadcast 向所有配对会话投递 HTML 文本。
func (s *eventSink) broadcast(ctx context.Context, text string) {
	if text == "" {
		return
	}
	for _, chatID := range s.c.pairedChats() {
		_ = s.client.sendMessage(ctx, chatID, text)
	}
}

// sendInteractive 发带键盘的交互消息并记录 message id,供 terminalize 原地改写。
func (s *eventSink) sendInteractive(ctx context.Context, id, text string, keyboard inlineKeyboardMarkup) {
	for _, chatID := range s.c.pairedChats() {
		msgID, err := s.client.sendMessageWithKeyboard(ctx, chatID, text, keyboard)
		if err != nil || id == "" {
			continue
		}
		s.c.mu.Lock()
		s.c.pendingMsgs[id] = msgRef{chatID: chatID, messageID: msgID, text: text}
		s.c.mu.Unlock()
	}
}

// approvalKeyboard 把意图携带的协议层 action 渲染成单行内联键盘;
// callback data 为 "<actionID>:<approvalID>"。
func approvalKeyboard(in connect.Intent) inlineKeyboardMarkup {
	row := make([]inlineKeyboardButton, 0, len(in.Actions))
	for _, a := range in.Actions {
		row = append(row, inlineKeyboardButton{Text: a.Label, CallbackData: a.ID + ":" + in.ID})
	}
	return inlineKeyboardMarkup{InlineKeyboard: [][]inlineKeyboardButton{row}}
}

// questionKeyboard 把意图携带的协议层 action 渲染成内联键盘:选项按钮
// (多选标 ✅ 表示当前选择)加确认/忽略行;单选点按即提交。自由作答的
// 问题不带 action,返回 false(走 /answer 文本作答)。
func (c *Connector) questionKeyboard(in connect.Intent) (inlineKeyboardMarkup, bool) {
	if in.Question == nil || len(in.Actions) == 0 {
		return inlineKeyboardMarkup{}, false
	}
	multi := in.Question.Questions[0].Multiple
	c.mu.Lock()
	selected := c.pendingSelect[in.ID]
	c.mu.Unlock()
	rows := make([][]inlineKeyboardButton, 0, len(in.Actions))
	var tail []inlineKeyboardButton
	for _, a := range in.Actions {
		switch a.ID {
		case connect.ActionConfirm:
			tail = append(tail, inlineKeyboardButton{Text: a.Label, CallbackData: "answer:" + in.ID + ":" + connect.ActionConfirm})
		case connect.ActionDismiss:
			tail = append(tail, inlineKeyboardButton{Text: a.Label, CallbackData: connect.ActionDismiss + ":" + in.ID})
		default: // 选项,action ID 即选项下标
			label := a.Label
			if idx, err := strconv.Atoi(a.ID); err == nil && multi && selected[idx] {
				label = "✅ " + label
			}
			rows = append(rows, []inlineKeyboardButton{{
				Text:         label,
				CallbackData: fmt.Sprintf("answer:%s:%s", in.ID, a.ID),
			}})
		}
	}
	if len(tail) > 0 {
		rows = append(rows, tail)
	}
	return inlineKeyboardMarkup{InlineKeyboard: rows}, true
}

// pairedChats returns the chats of the active profile to broadcast to.
func (c *Connector) pairedChats() []int64 {
	cfg, err := loadConfig()
	if err != nil {
		return nil
	}
	profile, ok := cfg.activeProfile()
	if !ok {
		return nil
	}
	return profile.pairedChatIDs()
}

// terminalize rewrites an interactive message into its verdict form (status
// line appended, keyboard stripped). First-answer-wins: the loser surface is
// terminalized with the canonical verdict rather than showing an error.
// Idempotent — unknown or already-terminalized IDs are no-ops.
func (c *Connector) terminalize(ctx context.Context, client *apiClient, id, status string) {
	c.mu.Lock()
	ref, ok := c.pendingMsgs[id]
	delete(c.pendingMsgs, id)
	delete(c.pendingSelect, id)
	c.mu.Unlock()
	if !ok {
		return
	}
	body := ref.text + "\n\n<b>✅ " + taskview.HTMLEscape(status) + "</b>"
	_ = client.editMessage(ctx, ref.chatID, ref.messageID, body, &emptyKeyboard)
}

// DeliverFile implements protocol.FileDeliverer: sends a local file as a
// document message to every paired chat.
func (c *Connector) DeliverFile(ctx context.Context, path string) error {
	c.mu.Lock()
	client := c.api
	c.mu.Unlock()
	if client == nil {
		return fmt.Errorf("telegram connector is not running")
	}
	chats := c.pairedChats()
	if len(chats) == 0 {
		return fmt.Errorf("no paired chat")
	}
	var errs []error
	for _, chatID := range chats {
		if err := client.sendDocument(ctx, chatID, path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *Connector) tracker() *taskview.Tracker {
	return c.taskTracker
}
