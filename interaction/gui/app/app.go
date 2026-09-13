// Package app adapts runtime.Runtime to the Wails binding surface.
// Runtime events are projected to Wails events consumed by the frontend.
//
// 事件协议 (Run-form):
//
//	agent:delta         { id, delta, done }                   — 流式文本增量
//	agent:reasoning     { delta, done }                       — reasoning 增量
//	agent:phase         { phase }                             — Agent phase 变化
//	agent:tool_start    { id, toolName, args, preview }       — 工具开始 (含 _preview)
//	agent:tool_blocked  { id, toolName, args, reason }       — 工具被钩子/策略阻塞
//	agent:tool_preview  { toolName, preview }                — edit 等的格式化预览替换
//	agent:tool_done     { toolName, args, output, isError }  — 工具完成
//	agent:subagent_start { id, subType, colorIdx }            — 子代理开始
//	agent:subagent_end   { id }                               — 子代理结束
//	agent:todos         { items }                             — Todo 列表更新
//	agent:metrics       { prompt, completion, cacheHit, ... } — Run 结束时的统计
//	agent:status        { status }                            — UI 顶层状态 (idle/thinking/running)
//	agent:done          { output, error }                    — Run 完结
//	agent:question      { id, questions }                     — 用户问题请求
//	agent:step          {...}                                  — 兜底: 未分发的 action
package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	controlruntime "nekocode/runtime"
	"nekocode/runtime/standard"

	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是绑定到 Wails 前端的应用实例。
type App struct {
	ctx   context.Context
	rt    runtimeClient
	mu    sync.Mutex
	runs  int
	ready atomic.Bool
	start time.Time
}

type runtimeClient interface {
	controlruntime.Interaction
	CurrentModel() controlruntime.ModelSelection
	SwitchModel(string) (controlruntime.ModelSelection, error)
	ContextSnapshot() controlruntime.ContextSnapshot
	MemoryView(controlruntime.MemoryScope) controlruntime.MemoryView
	SkillManagementView() controlruntime.SkillManagementView
	SelectSkill(string) error
	ClearSelectedSkill() error
	RefreshSkillManagement() (controlruntime.SkillManagementView, error)
	SetPluginEnabled(string, bool) (controlruntime.SkillManagementView, error)
	ConfigView() controlruntime.ConfigView
	ResolveModelProfile(controlruntime.ModelSpec) controlruntime.ModelProfile
	ApplyConfig(controlruntime.ConfigView) (controlruntime.ConfigView, error)
	CurrentSessionID() string
	ListSessions() []controlruntime.SessionMeta
	SessionMessages() []controlruntime.DisplayMessage
	ResumeSession(string) error
	NewSession() (controlruntime.SessionMeta, error)
	DeleteSession(string) error
	Close() error
}

// NewApp creates the Wails adapter and its standard runtime.
func NewApp() (*App, error) {
	rt, err := standard.New()
	if err != nil {
		return nil, err
	}
	return &App{
		rt: rt,
	}, nil
}

// ---------- Wails 生命周期 ----------

// Startup 在 Wails 窗口启动时调用。
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.ready.Store(true)
	wailsruntime.LogInfo(ctx, "NekoCode GUI started, runtime ready")
}

// Shutdown 在窗口关闭时调用。
func (a *App) Shutdown(_ context.Context) {
	wailsruntime.LogInfo(a.ctx, "NekoCode GUI shutting down")
	if err := a.rt.Close(); err != nil {
		wailsruntime.LogError(a.ctx, "runtime shutdown failed: "+err.Error())
	}
}

// DomReady 在前端 DOM 就绪时调用。
func (a *App) DomReady(_ context.Context) {
	wailsruntime.LogInfo(a.ctx, "Frontend DOM ready")
	events, err := a.rt.Events(a.ctx, controlruntime.EventFilter{})
	if err != nil {
		wailsruntime.LogError(a.ctx, "runtime subscribe failed: "+err.Error())
		return
	}
	go func() {
		for ev := range events {
			a.dispatchRuntimeEvent(ev)
		}
	}()
}

// compactConfirmArgs 提取确认弹窗需要显示的 args。
func compactConfirmArgs(req controlruntime.ConfirmRequest) map[string]any {
	m := make(map[string]any, 4)
	switch req.ToolName {
	case "edit":
		if p, ok := req.Args["path"].(string); ok {
			m["path"] = p
		}
		if old, ok := req.Args["oldString"].(string); ok {
			m["oldString"] = truncateConfirmString(old)
		}
		if next, ok := req.Args["newString"].(string); ok {
			m["newString"] = truncateConfirmString(next)
		}
		if replaceAll, ok := req.Args["replaceAll"].(bool); ok {
			m["replaceAll"] = replaceAll
		}
	case "write":
		if p, ok := req.Args["path"].(string); ok {
			m["path"] = p
		}
		if c, ok := req.Args["content"].(string); ok && len(c) > 200 {
			m["content"] = c[:200] + "..."
		} else {
			m["content"] = c
		}
	default:
		for k, v := range req.Args {
			if k == "_preview" {
				continue
			}
			if s, ok := v.(string); ok && len(s) > 200 {
				m[k] = s[:200] + "..."
			} else if k == "content" || k == "path" || k == "command" {
				m[k] = v
			}
		}
	}
	return m
}

func truncateConfirmString(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

func confirmPreview(req controlruntime.ConfirmRequest) string {
	if p, ok := req.Args["_preview"].(string); ok {
		return p
	}
	return ""
}

// ---------- 前端可调用的 Method ----------

// CommandMenu returns the transport-neutral picker for the current input.
// Nil means the input has no finite choices and should remain normal text.
func (a *App) CommandMenu(input string) *controlruntime.CommandMenu {
	menu, ok := a.rt.CommandMenu(a.ctx, strings.TrimSpace(input))
	if !ok {
		return nil
	}
	return &menu
}

// SendMessage 发送一条用户消息并启动 Agent 循环。
func (a *App) SendMessage(input string) {
	a.mu.Lock()
	a.runs++
	a.start = time.Now()
	a.mu.Unlock()

	wailsruntime.EventsEmit(a.ctx, "agent:status", map[string]string{
		"status": "thinking",
	})
	_, err := a.rt.StartRun(a.ctx, controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "gui"},
		Text:   input,
	})
	if err != nil {
		wailsruntime.EventsEmit(a.ctx, "agent:done", map[string]string{
			"output": "",
			"error":  err.Error(),
		})
		wailsruntime.EventsEmit(a.ctx, "agent:status", map[string]string{
			"status": "idle",
		})
	}
}

func (a *App) dispatchRuntimeEvent(ev controlruntime.Event) {
	switch ev.Type {
	case controlruntime.EventCompaction:
		if p, ok := ev.Payload.(controlruntime.CompactionPayload); ok {
			wailsruntime.EventsEmit(a.ctx, "agent:compaction", p)
		}
	case controlruntime.EventRunStarted:
		wailsruntime.EventsEmit(a.ctx, "agent:status", map[string]string{"status": "running"})
	case controlruntime.EventAssistantDelta:
		if p, ok := ev.Payload.(controlruntime.DeltaPayload); ok {
			wailsruntime.EventsEmit(a.ctx, "agent:delta", map[string]any{
				"id":    a.runNumber(),
				"delta": p.Delta,
				"done":  false,
			})
		}
	case controlruntime.EventReasoningDelta:
		if p, ok := ev.Payload.(controlruntime.DeltaPayload); ok {
			wailsruntime.EventsEmit(a.ctx, "agent:reasoning", map[string]any{
				"delta": p.Delta,
				"done":  false,
			})
		}
	case controlruntime.EventPhaseChanged:
		if p, ok := ev.Payload.(controlruntime.PhasePayload); ok {
			// Frontend AgentPhase keys are lowercase; the runtime constants are
			// capitalized ("Thinking", ...). Normalize at the emit boundary.
			wailsruntime.EventsEmit(a.ctx, "agent:phase", map[string]string{"phase": strings.ToLower(p.Phase)})
		}
	case controlruntime.EventTodosUpdated:
		wailsruntime.EventsEmit(a.ctx, "agent:todos", map[string]any{"items": ev.Payload})
	case controlruntime.EventSessionChanged:
		if p, ok := ev.Payload.(controlruntime.SessionPayload); ok {
			wailsruntime.EventsEmit(a.ctx, "session:changed", map[string]any{
				"id": p.ID, "messages": a.rt.SessionMessages(),
			})
		}
	case controlruntime.EventMetricsUpdated:
		if metrics, ok := ev.Payload.(controlruntime.MetricsSnapshot); ok {
			wailsruntime.EventsEmit(a.ctx, "agent:metrics", map[string]any{
				"prompt": metrics.TurnPrompt, "completion": metrics.TurnCompletion,
				"elapsedMs":    time.Since(a.startTime()).Milliseconds(),
				"compactCount": metrics.CompactCount,
			})
		}
	case controlruntime.EventToolStarted:
		if p, ok := ev.Payload.(controlruntime.ToolPayload); ok {
			a.dispatchToolEvent(ev.Type, p)
		}
	case controlruntime.EventToolBlocked:
		if p, ok := ev.Payload.(controlruntime.ToolPayload); ok {
			a.dispatchToolEvent(ev.Type, p)
		}
	case controlruntime.EventToolPreview:
		if p, ok := ev.Payload.(controlruntime.ToolPayload); ok {
			a.dispatchToolEvent(ev.Type, p)
		}
	case controlruntime.EventToolCompleted:
		if p, ok := ev.Payload.(controlruntime.ToolPayload); ok {
			a.dispatchToolEvent(ev.Type, p)
		}
	case controlruntime.EventSubAgentStarted:
		if p, ok := ev.Payload.(controlruntime.SubAgentPayload); ok {
			profile := p.Profile
			if profile == "" {
				profile = p.Type
			}
			wailsruntime.EventsEmit(a.ctx, "agent:subagent_start", map[string]any{
				"id": p.ID, "subType": profile, "profile": profile,
				"skills": p.Skills, "colorIdx": p.Color,
			})
		}
	case controlruntime.EventSubAgentEnded:
		if p, ok := ev.Payload.(controlruntime.SubAgentPayload); ok {
			wailsruntime.EventsEmit(a.ctx, "agent:subagent_end", map[string]any{"id": p.ID})
		}
	case controlruntime.EventApprovalRequested:
		if p, ok := ev.Payload.(controlruntime.ApprovalView); ok {
			req := p.ToConfirmRequest()
			wailsruntime.EventsEmit(a.ctx, "agent:confirm", map[string]any{
				"id":       p.ID,
				"toolName": req.ToolName,
				"args":     compactConfirmArgs(req),
				"preview":  confirmPreview(req),
				"kind":     string(req.Kind),
				"approval": req.Approval,
			})
		}
	case controlruntime.EventQuestionRequested:
		if p, ok := ev.Payload.(controlruntime.QuestionView); ok {
			wailsruntime.EventsEmit(a.ctx, "agent:question", map[string]any{
				"id":        p.ID,
				"questions": p.Questions,
			})
		}
	case controlruntime.EventRunDone:
		payload, _ := ev.Payload.(controlruntime.RunResult)
		a.emitRunDone(payload.Output, "")
	case controlruntime.EventSystemMessage:
		// Command output (e.g. /connect, /model) reaches the UI as a
		// system message so the user sees the command's reply.
		if p, ok := ev.Payload.(controlruntime.MessagePayload); ok && strings.TrimSpace(p.Content) != "" {
			wailsruntime.EventsEmit(a.ctx, "agent:system", map[string]any{
				"content": p.Content,
			})
		}
	case controlruntime.EventRunFailed:
		payload, _ := ev.Payload.(controlruntime.RunResult)
		a.emitRunDone(payload.Output, payload.Error)
	case controlruntime.EventRunCancelled:
		a.emitRunDone("", "cancelled")
	case controlruntime.EventConnectorStatus:
		if p, ok := ev.Payload.(controlruntime.ConnectorStatusPayload); ok {
			wailsruntime.EventsEmit(a.ctx, "agent:step", map[string]string{
				"action":   "connector_status",
				"toolName": p.Name,
				"output":   p.Message,
			})
		}
	}
}

func (a *App) emitRunDone(output, errStr string) {
	wailsruntime.EventsEmit(a.ctx, "agent:delta", map[string]any{
		"id":    a.runNumber(),
		"delta": "",
		"done":  true,
	})
	wailsruntime.EventsEmit(a.ctx, "agent:done", map[string]string{
		"output": output,
		"error":  errStr,
	})
	wailsruntime.EventsEmit(a.ctx, "agent:status", map[string]string{
		"status": "idle",
	})
}

func (a *App) runNumber() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs
}

func (a *App) startTime() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.start.IsZero() {
		return time.Now()
	}
	return a.start
}

func (a *App) dispatchToolEvent(eventType controlruntime.EventType, p controlruntime.ToolPayload) {
	switch eventType {
	case controlruntime.EventToolStarted:
		wailsruntime.EventsEmit(a.ctx, "agent:tool_start", map[string]any{
			"id":            toolEventID(p),
			"toolName":      p.ToolName,
			"args":          p.Args,
			"preview":       p.Preview,
			"blocked":       false,
			"subAgentId":    p.SubAgentID,
			"subAgentColor": p.SubAgentColor,
		})
	case controlruntime.EventToolBlocked:
		wailsruntime.EventsEmit(a.ctx, "agent:tool_start", map[string]any{
			"id":       toolEventID(p),
			"toolName": p.ToolName,
			"args":     p.Args,
			"preview":  p.Output,
			"blocked":  true,
			"reason":   p.Output,
		})
	case controlruntime.EventToolPreview:
		wailsruntime.EventsEmit(a.ctx, "agent:tool_preview", map[string]any{
			"toolName": p.ToolName,
			"preview":  p.Preview,
		})
	case controlruntime.EventToolCompleted:
		wailsruntime.EventsEmit(a.ctx, "agent:tool_done", map[string]any{
			"toolName":      p.ToolName,
			"args":          p.Args,
			"output":        p.Output,
			"isError":       p.IsError,
			"id":            toolEventID(p),
			"subAgentId":    p.SubAgentID,
			"subAgentColor": p.SubAgentColor,
		})
	}
}

// toolEventID returns the tool call ID carried by the event, falling back to a
// fresh UUID for events that legitimately lack one (e.g. previews).
func toolEventID(p controlruntime.ToolPayload) string {
	if p.CallID != "" {
		return p.CallID
	}
	return uuid.NewString()
}

func (a *App) Abort() {
	_ = a.rt.CancelRun(a.ctx, "")
	wailsruntime.EventsEmit(a.ctx, "agent:status", map[string]string{
		"status": "idle",
	})
}

func (a *App) CurrentModel() string {
	selection := a.rt.CurrentModel()
	if selection.Provider == "" {
		return ""
	}
	return selection.Provider + "|" + selection.Model
}

func (a *App) SwitchModel(name string) (string, error) {
	selection, err := a.rt.SwitchModel(name)
	if err != nil {
		return "", err
	}
	return selection.Provider + "|" + selection.Model, nil
}

func (a *App) ContextSnapshot() controlruntime.ContextSnapshot {
	return a.rt.ContextSnapshot()
}

func (a *App) MemoryView(scope string) controlruntime.MemoryView {
	return a.rt.MemoryView(controlruntime.MemoryScope(scope))
}

func (a *App) SelectSkill(name string) error {
	return a.rt.SelectSkill(name)
}

func (a *App) ClearSelectedSkill() {
	_ = a.rt.ClearSelectedSkill()
}

func (a *App) GetConfig() controlruntime.ConfigView {
	return a.rt.ConfigView()
}

func (a *App) ResolveModelProfile(model controlruntime.ModelSpec) controlruntime.ModelProfile {
	return a.rt.ResolveModelProfile(model)
}

func (a *App) SaveConfig(cfg controlruntime.ConfigView) (controlruntime.ConfigView, error) {
	next, err := a.rt.ApplyConfig(cfg)
	if err != nil {
		return controlruntime.ConfigView{}, err
	}
	return next, nil
}

func (a *App) GetSkillManagement() controlruntime.SkillManagementView {
	return a.rt.SkillManagementView()
}

func (a *App) RefreshSkillManagement() controlruntime.SkillManagementView {
	view, _ := a.rt.RefreshSkillManagement()
	return view
}

func (a *App) SetPluginEnabled(name string, enabled bool) (controlruntime.SkillManagementView, error) {
	next, err := a.rt.SetPluginEnabled(name, enabled)
	if err != nil {
		return controlruntime.SkillManagementView{}, err
	}
	return next, nil
}

// ---------- Session 管理 ----------

// ListSessions 返回所有已落盘的会话元数据。
func (a *App) ListSessions() []controlruntime.SessionMeta {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rt.ListSessions()
}

// NewSession 创建一个新会话并将其设为当前会话，返回会话元数据。
func (a *App) NewSession() (controlruntime.SessionMeta, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	meta, err := a.rt.NewSession()
	if err != nil {
		return controlruntime.SessionMeta{}, err
	}
	return meta, nil
}

func (a *App) LoadSession(id string) ([]controlruntime.DisplayMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.rt.ResumeSession(id); err != nil {
		return nil, err
	}
	return a.rt.SessionMessages(), nil
}

// DeleteSession 删除指定会话。若删除的是当前会话，会清空上下文并等待下一次真实对话再创建会话。
func (a *App) DeleteSession(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rt.DeleteSession(id)
}

func (a *App) ReadImageBase64(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	home, _ := os.UserHomeDir()
	nekocodeDir := filepath.Join(home, ".nekocode")
	cwd := a.currentSessionCWD()

	ext := strings.ToLower(filepath.Ext(abs))
	var mime string
	switch ext {
	case ".png":
		mime = "image/png"
	case ".gif":
		mime = "image/gif"
	case ".webp":
		mime = "image/webp"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	default:
		return "", fmt.Errorf("unsupported image type: %s", ext)
	}

	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve image target: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat image: %w", err)
	}
	realInfo, err := os.Stat(realPath)
	if err != nil || !os.SameFile(info, realInfo) {
		return "", fmt.Errorf("image target changed while opening: %s", abs)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("image is not a regular file: %s", abs)
	}
	if !pathWithin(cwd, realPath) && !pathWithin(nekocodeDir, realPath) {
		return "", fmt.Errorf("path outside allowed directories: %s", abs)
	}

	data, err := io.ReadAll(io.LimitReader(f, 20<<20))
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mime, encoded), nil
}

func pathWithin(dir, target string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realDir, target)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (a *App) currentSessionCWD() string {
	current := a.rt.CurrentSessionID()
	for _, session := range a.rt.ListSessions() {
		if session.ID == current {
			return session.CWD
		}
	}
	return ""
}

// ReplyConfirm 由前端调用，回复确认弹窗。
func (a *App) ReplyConfirm(id string, ok bool) {
	a.ReplyConfirmDecision(id, ok, false)
}

// ReplyConfirmDecision 由前端调用，回复统一审批并可选择记住项目级权限。
// 请求卡已经列出当前命令和可预测的沙箱能力；一次决定原子覆盖两者。
func (a *App) ReplyConfirmDecision(id string, ok bool, remember bool) {
	_ = a.rt.DecideApproval(a.ctx, id, controlruntime.ApprovalDecision{
		Allowed:  ok,
		Remember: ok && remember,
	})
}

// ReplyQuestion 由前端调用，回复 agent 发起的问题。
func (a *App) ReplyQuestion(id string, answersJSON string, rejected bool) {
	var answers [][]string
	if answersJSON != "" {
		_ = json.Unmarshal([]byte(answersJSON), &answers)
	}
	_ = a.rt.AnswerQuestion(a.ctx, id, controlruntime.QuestionReply{Answers: answers, Rejected: rejected})
}
