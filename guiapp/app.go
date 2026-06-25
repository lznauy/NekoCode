// app.go — Wails App 桥接层: 将 bot.Bot 的核心能力暴露给前端。
// 流式对话通过 Wails Events 推送，前端用 EventsOn 接收。
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
//	agent:step          {...}                                  — 兜底: 未分发的 action
package guiapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/bot"
	"nekocode/bot/session"
	"nekocode/common"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是绑定到 Wails 前端的应用实例。
type App struct {
	ctx   context.Context
	bot   *bot.Bot
	mu    sync.Mutex
	runs  int
	ready atomic.Bool

	// pendingTools 按 toolName 排队保存新生成的工具 id, 用于 tool_start ↔ tool_preview ↔ tool_done 关联。
	pendingMu sync.Mutex
	pending   map[string][]string

	// confirm 确认弹窗
	confirmMu sync.Mutex
	confirmCh chan common.ConfirmRequest
	confs     map[string]common.ConfirmRequest // id -> req, 等待前端回复
}

// NewApp 创建 App 实例，bot.Bot 在这里初始化以消除 startup/domReady 竞态。
func NewApp() *App {
	return &App{
		bot:       bot.New(),
		pending:   make(map[string][]string),
		confs:     make(map[string]common.ConfirmRequest),
		confirmCh: make(chan common.ConfirmRequest),
	}
}

// ---------- Wails 生命周期 ----------

// Startup 在 Wails 窗口启动时调用。
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.ready.Store(true)
	runtime.LogInfo(ctx, "NekoCode GUI started, bot ready")
}

// Shutdown 在窗口关闭时调用。
func (a *App) Shutdown(_ context.Context) {
	runtime.LogInfo(a.ctx, "NekoCode GUI shutting down")
}

// DomReady 在前端 DOM 就绪时调用。
func (a *App) DomReady(_ context.Context) {
	runtime.LogInfo(a.ctx, "Frontend DOM ready")
	phaseFn := func(phase string) {
		runtime.EventsEmit(a.ctx, "agent:phase", map[string]string{"phase": phase})
	}
	todoFn := func(items []common.TodoItem) {
		runtime.EventsEmit(a.ctx, "agent:todos", map[string]any{
			"items": items,
		})
	}
	confirmFn := func(req common.ConfirmRequest) bool {
		// 用 uuid 做 key，通过 Wails event 推给前端。
		id := uuid.NewString()
		a.confirmMu.Lock()
		a.confs[id] = req
		a.confirmMu.Unlock()
		runtime.EventsEmit(a.ctx, "agent:confirm", map[string]any{
			"id":       id,
			"toolName": req.ToolName,
			"args":     compactConfirmArgs(req),
			"level":    int(req.Level),
		})
		// 阻塞等前端调 ReplyConfirm 写回。
		resp := <-req.Response
		return resp
	}
	a.bot.Configure(confirmFn, phaseFn, todoFn, nil, a.confirmCh)
}

// compactConfirmArgs 提取确认弹窗需要显示的 args。
func compactConfirmArgs(req common.ConfirmRequest) map[string]any {
	m := make(map[string]any, 4)
	switch req.ToolName {
	case "edit":
		if p, ok := req.Args["path"].(string); ok {
			m["path"] = p
		}
		if p, ok := req.Args["patch"].(string); ok && len(p) > 200 {
			m["patch"] = p[:200] + "..."
		} else {
			m["patch"] = p
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
			if s, ok := v.(string); ok && len(s) > 200 {
				m[k] = s[:200] + "..."
			} else if k == "patch" || k == "content" || k == "path" || k == "command" {
				m[k] = v
			}
		}
	}
	return m
}

// ---------- 工具 id 关联 ----------

func (a *App) popPendingTool(toolName string) (string, bool) {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	if queue, ok := a.pending[toolName]; ok && len(queue) > 0 {
		id := queue[0]
		a.pending[toolName] = queue[1:]
		return id, false
	}
	return uuid.NewString(), true
}

func (a *App) pushPendingTool(toolName, id string) {
	a.pendingMu.Lock()
	a.pending[toolName] = append(a.pending[toolName], id)
	a.pendingMu.Unlock()
}

func (a *App) resetPending() {
	a.pendingMu.Lock()
	a.pending = make(map[string][]string)
	a.pendingMu.Unlock()
}

// ---------- 前端可调用的 Method ----------

// SendMessage 发送一条用户消息并启动 Agent 循环。
func (a *App) SendMessage(input string) {
	a.mu.Lock()
	a.runs++
	runID := a.runs
	a.mu.Unlock()

	a.resetPending()

	a.bot.SetCallbacks(
		func(delta string) {
			runtime.EventsEmit(a.ctx, "agent:delta", map[string]any{
				"id":    runID,
				"delta": delta,
				"done":  false,
			})
		},
		func(delta string) {
			runtime.EventsEmit(a.ctx, "agent:reasoning", map[string]any{
				"delta": delta,
				"done":  false,
			})
		},
	)

	onStep := func(action, toolName, toolArgs, output string) {
		a.dispatchStep(action, toolName, toolArgs, output)
	}

	start := time.Now()
	go func() {
		runtime.EventsEmit(a.ctx, "agent:status", map[string]string{
			"status": "thinking",
		})

		result, err := a.bot.RunAgent(input, onStep)
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}

		runtime.EventsEmit(a.ctx, "agent:delta", map[string]any{
			"id":    runID,
			"delta": "",
			"done":  true,
		})

		stats := a.bot.Stats()
		runtime.EventsEmit(a.ctx, "agent:metrics", map[string]any{
			"prompt":       stats.PromptTokens,
			"completion":   stats.CompletionTokens,
			"cacheHit":     0,
			"cacheMiss":    0,
			"elapsedMs":    time.Since(start).Milliseconds(),
			"compactCount": stats.CompactCount,
		})

		runtime.EventsEmit(a.ctx, "agent:done", map[string]string{
			"output": result,
			"error":  errStr,
		})

		runtime.EventsEmit(a.ctx, "agent:status", map[string]string{
			"status": "idle",
		})
	}()
}

func (a *App) dispatchStep(action, toolName, toolArgs, output string) {
	switch action {
	case "tool_start":
		id := uuid.NewString()
		a.pushPendingTool(toolName, id)
		runtime.EventsEmit(a.ctx, "agent:tool_start", map[string]any{
			"id":       id,
			"toolName": toolName,
			"args":     toolArgs,
			"preview":  output,
			"blocked":  false,
		})
	case "tool_blocked":
		id := uuid.NewString()
		a.pushPendingTool(toolName, id)
		runtime.EventsEmit(a.ctx, "agent:tool_start", map[string]any{
			"id":       id,
			"toolName": toolName,
			"args":     toolArgs,
			"preview":  output,
			"blocked":  true,
			"reason":   output,
		})
	case "tool_preview":
		runtime.EventsEmit(a.ctx, "agent:tool_preview", map[string]any{
			"toolName": toolName,
			"preview":  output,
		})
	case "execute_tool":
		id, _ := a.popPendingTool(toolName)
		isError := false
		if toolName == "edit" {
			isError = !strings.HasPrefix(strings.TrimSpace(output), "[")
		}
		runtime.EventsEmit(a.ctx, "agent:tool_done", map[string]any{
			"toolName": toolName,
			"args":     toolArgs,
			"output":   output,
			"isError":  isError,
			"id":       id,
		})
	case "sub_agent_start":
		runtime.EventsEmit(a.ctx, "agent:subagent_start", map[string]any{
			"id":       toolArgs,
			"subType":  toolName,
			"colorIdx": parseIntSafe(output),
		})
	case "sub_agent_end":
		runtime.EventsEmit(a.ctx, "agent:subagent_end", map[string]any{
			"id": toolArgs,
		})
	case "think":
		runtime.EventsEmit(a.ctx, "agent:reasoning", map[string]any{
			"delta": "",
			"done":  true,
		})
	default:
		runtime.EventsEmit(a.ctx, "agent:step", map[string]string{
			"action":   action,
			"toolName": toolName,
			"toolArgs": toolArgs,
			"output":   output,
		})
	}
}

func parseIntSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func (a *App) Abort() {
	if a.bot != nil {
		a.bot.Abort()
		runtime.EventsEmit(a.ctx, "agent:status", map[string]string{
			"status": "idle",
		})
	}
}

func (a *App) ProviderModel() string {
	p, m := a.bot.ProviderModel()
	if p == "" {
		return ""
	}
	return p + "|" + m
}

// ---------- Session 管理 ----------

// ListSessions 返回所有已落盘的会话元数据，如果为空则加上当前内存会话（不落盘）。
func (a *App) ListSessions() []session.Meta {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := session.List()
	if len(list) == 0 {
		// initSession 里已创建内存会话，返回其元数据（不落盘）。
		if sid := a.bot.CurrentSessionID(); sid != "" {
			return []session.Meta{{
				ID:        sid,
				CWD:       a.bot.CWD(),
				CreatedAt: time.Now().Unix(),
				UpdatedAt: time.Now().Unix(),
				MsgCount:  0,
			}}
		}
	}
	return list
}

// NewSession 创建一个新会话并将其设为当前会话，返回会话元数据。
// 不落盘——等发送第一条消息后 saveSession 才写文件。
func (a *App) NewSession() (session.Meta, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sess, err := session.New(a.bot.CWD())
	if err != nil {
		return session.Meta{}, err
	}

	a.bot.ClearContext()
	a.bot.SetSession(sess)

	return sessionMeta(sess), nil
}

func (a *App) LoadSession(id string) ([]common.DisplayMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.bot.ResumeSession(id); err != nil {
		return nil, err
	}
	return a.bot.SessionMessages(), nil
}

// DeleteSession 删除指定会话。若删除的是当前会话，会后端内存里新建一个（不落盘）。
func (a *App) DeleteSession(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := session.Delete(id); err != nil {
		return err
	}

	if a.bot.CurrentSessionID() == id {
		sess, err := session.New(a.bot.CWD())
		if err != nil {
			return err
		}
		a.bot.ClearContext()
		a.bot.SetSession(sess)
	}

	return nil
}

func (a *App) ReadImageBase64(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	home, _ := os.UserHomeDir()
	nekocodeDir := filepath.Join(home, ".nekocode")
	cwd := a.bot.CWD()

	allowed := func(dir, target string) bool {
		rel, err := filepath.Rel(dir, target)
		if err != nil {
			return false
		}
		return !strings.Contains(rel, "..")
	}

	if !allowed(cwd, abs) && !allowed(nekocodeDir, abs) {
		return "", fmt.Errorf("path outside allowed directories: %s", abs)
	}

	ext := strings.ToLower(filepath.Ext(abs))
	mime := "image/jpeg"
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
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, 20<<20))
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mime, encoded), nil
}

func sessionMeta(sess *session.Snapshot) session.Meta {
	return session.Meta{
		ID:        sess.ID,
		CWD:       sess.CWD,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: sess.UpdatedAt,
		MsgCount:  len(sess.Messages),
	}
}

// ReplyConfirm 由前端调用，回复确认弹窗。
func (a *App) ReplyConfirm(id string, ok bool) {
	a.confirmMu.Lock()
	req, found := a.confs[id]
	if found {
		delete(a.confs, id)
	}
	a.confirmMu.Unlock()
	if found {
		req.Response <- ok
	}
}
