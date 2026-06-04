package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nekocode/common"
)

type Executor struct {
	registry  *Registry
	confirmFn common.ConfirmFunc
	phaseFn   common.PhaseFunc
	planMode  bool
	readFiles map[string]bool
	readMu    sync.RWMutex
	previewFn func(toolName string, args map[string]any, preview string)
}

func NewExecutor(r *Registry) *Executor {
	return &Executor{registry: r, readFiles: make(map[string]bool)}
}

func (e *Executor) SetConfirmFn(fn common.ConfirmFunc) { e.confirmFn = fn }
func (e *Executor) ConfirmFn() common.ConfirmFunc     { return e.confirmFn }
func (e *Executor) SetPhaseFn(fn common.PhaseFunc)      { e.phaseFn = fn }
func (e *Executor) SetPlanMode(on bool)                 { e.planMode = on }
func (e *Executor) SetPreviewFn(fn func(string, map[string]any, string)) { e.previewFn = fn }

func (e *Executor) ExecuteBatch(ctx context.Context, calls []ToolCallItem) []ToolCallResult {
	var ro, mw []ToolCallItem
	for _, c := range calls {
		if t, err := e.registry.Get(c.Name); err == nil && t.ExecutionMode(c.Args) == ModeParallel {
			ro = append(ro, c)
		} else {
			mw = append(mw, c)
		}
	}

	results := make([]ToolCallResult, len(calls))
	n := 0

	// Read-only: parallel.
	if len(ro) > 0 {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 10)
		for i, c := range ro {
			if ctx.Err() != nil {
				results[n+i] = ToolCallResult{ID: c.ID, Name: c.Name, Error: ctx.Err().Error()}
				continue
			}
			wg.Add(1)
			go func(idx int, tc ToolCallItem) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				results[n+idx] = e.executeOne(ctx, tc)
			}(i, c)
		}
		wg.Wait()
		n += len(ro)
	}

	// Mutable: sequential.
	for i, c := range mw {
		if t, err := e.registry.Get(c.Name); err == nil {
			if p, ok := t.(interface{ Preview(map[string]any) string }); ok {
				preview := p.Preview(c.Args)
				c.Args["_preview"] = preview
				if e.previewFn != nil {
					e.previewFn(c.Name, c.Args, preview)
				}
			}
		}
		results[n+i] = e.executeOne(ctx, c)
	}
	return results
}

func (e *Executor) executeOne(ctx context.Context, tc ToolCallItem) ToolCallResult {
	tool, err := e.registry.Get(tc.Name)
	if err != nil {
		return ToolCallResult{ID: tc.ID, Name: tc.Name, Error: err.Error()}
	}

	level := tool.DangerLevel(tc.Args)
	if level == common.LevelForbidden {
		return ToolCallResult{ID: tc.ID, Name: tc.Name, Error: "forbidden: " + tc.Name}
	}
	if e.phaseFn != nil {
		e.phaseFn(common.PhaseRunning + " " + tc.Name)
	}
	if e.planMode && level >= common.LevelWrite {
		return ToolCallResult{ID: tc.ID, Name: tc.Name, Error: "plan mode: blocked"}
	}
	if level >= common.LevelWrite && e.confirmFn != nil && !e.confirmFn(common.ConfirmRequest{
		ToolName: tc.Name, Args: tc.Args, Level: level, Response: make(chan bool, 1),
	}) {
		return ToolCallResult{ID: tc.ID, Name: tc.Name, Error: "cancelled"}
	}

	if tc.Name == "write" || tc.Name == "edit" {
		if p, _ := tc.Args["path"].(string); p != "" {
			if resolved, err := resolvePath(p); err == nil {
				if _, err := os.Stat(resolved); err == nil && !e.wasRead(resolved) {
					return ToolCallResult{ID: tc.ID, Name: tc.Name,
						Error: "file " + filepath.Base(resolved) + " has not been read yet"}
				}
			}
		}
	}

	var output string
		var execErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					execErr = fmt.Errorf("panic: %v", r)
				}
			}()
			output, execErr = tool.Execute(ctx, tc.Args)
		}()
		if execErr != nil {
			return ToolCallResult{ID: tc.ID, Name: tc.Name, Error: execErr.Error()}
		}
	output = truncateOutput(output)

	// Track reads + invalidate cache.
	if p, _ := tc.Args["path"].(string); p != "" {
		if resolved, err := resolvePath(p); err == nil {
			switch tc.Name {
			case "read":
				e.markRead(resolved)
			case "edit", "write":
				if GlobalFileCache != nil {
					GlobalFileCache.Invalidate(resolved)
				}
			}
		}
	}

	return ToolCallResult{ID: tc.ID, Name: tc.Name, Output: output}
}

func (e *Executor) markRead(path string) {
	e.readMu.Lock()
	e.readFiles[path] = true
	e.readMu.Unlock()
}

func (e *Executor) wasRead(path string) bool {
	e.readMu.RLock()
	defer e.readMu.RUnlock()
	return e.readFiles[path]
}

func resolvePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

const (
	maxLines = 2000
	headLen  = 40
	tailLen  = 20
)

func truncateOutput(output string) string {
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}

	tailStart := max(len(lines)-tailLen, headLen)

	var b strings.Builder
	for i := range headLen {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\n[... %d lines truncated ...]\n\n", tailStart-headLen)
	for i := range len(lines) - tailStart {
		b.WriteString(lines[tailStart+i])
		b.WriteByte('\n')
	}
	return b.String()
}
