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

// Previewer is an optional interface for tools that can generate a preview
// (e.g. a diff) before execution. Both PreparePreviews and ExecuteBatch use
// this interface to produce preview content for the TUI.
type Previewer interface {
	Preview(args map[string]any) string
}

type Executor struct {
	registry  *Registry
	confirmFn common.ConfirmFunc
	phaseFn   common.PhaseFunc
	planMode  bool
	readFiles map[string]bool
	readMu    sync.RWMutex
	previewFn func(toolName string, args map[string]any, preview string)
	fnMu      sync.RWMutex // protects confirmFn, phaseFn, previewFn (subagent engine mutates concurrently)
}

func NewExecutor(r *Registry) *Executor {
	return &Executor{registry: r, readFiles: make(map[string]bool)}
}

func (e *Executor) SetConfirmFn(fn common.ConfirmFunc) {
	e.fnMu.Lock()
	e.confirmFn = fn
	e.fnMu.Unlock()
}
func (e *Executor) ConfirmFn() common.ConfirmFunc {
	e.fnMu.RLock()
	defer e.fnMu.RUnlock()
	return e.confirmFn
}
func (e *Executor) SetPhaseFn(fn common.PhaseFunc) {
	e.fnMu.Lock()
	e.phaseFn = fn
	e.fnMu.Unlock()
}
func (e *Executor) SetPlanMode(on bool) {
	e.fnMu.Lock()
	e.planMode = on
	e.fnMu.Unlock()
}
func (e *Executor) SetPreviewFn(fn func(string, map[string]any, string)) {
	e.fnMu.Lock()
	e.previewFn = fn
	e.fnMu.Unlock()
}

// PreparePreviews runs Preview() on each mutable tool call and stores the
// result in Args["_preview"]. This allows callers to access previews before
// ExecuteBatch runs (e.g. for tool_start callbacks).
func (e *Executor) PreparePreviews(calls []ToolCallItem) {
	for i, c := range calls {
		if t, err := e.registry.Get(c.Name); err == nil {
			if p, ok := t.(Previewer); ok {
				calls[i].Args["_preview"] = p.Preview(c.Args)
			}
		}
	}
}

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
		sem := make(chan struct{}, 16)
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
			if p, ok := t.(Previewer); ok {
				// Reuse preview from PreparePreviews if available.
				preview, _ := c.Args["_preview"].(string)
				if preview == "" {
					preview = p.Preview(c.Args)
					c.Args["_preview"] = preview
				}
				e.fnMu.RLock()
				pfn := e.previewFn
				e.fnMu.RUnlock()
				if pfn != nil {
					pfn(c.Name, c.Args, preview)
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
	e.fnMu.RLock()
	pfn := e.phaseFn
	cfn := e.confirmFn
	pm := e.planMode
	e.fnMu.RUnlock()
	if pfn != nil {
		pfn(common.PhaseRunning + " " + tc.Name)
	}
	if pm && level >= common.LevelWrite {
		return ToolCallResult{ID: tc.ID, Name: tc.Name, Error: "plan mode: blocked"}
	}
	if level >= common.LevelWrite && cfn != nil && !cfn(common.NewConfirmRequest(tc.Name, confirmArgs(tc.Name, tc.Args), level)) {
		return ToolCallResult{ID: tc.ID, Name: tc.Name, Error: "cancelled"}
	}

	paths := toolPaths(tc)

	// Guard: tools that modify files must read them first. Bash is excluded
	// because extracting target file paths from arbitrary shell commands is
	// fragile — the DangerLevel confirmation system and forbidden-command
	// blocklist provide the primary safety layer for bash.
	if tc.Name == "write" || tc.Name == "edit" {
		for _, p := range paths {
			if resolved, err := ValidatePath(p); err == nil {
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
	for _, p := range paths {
		if resolved, err := ValidatePath(p); err == nil {
			switch tc.Name {
			case "read":
				e.markRead(resolved)
			case "write", "edit":
				if cache := GetGlobalFileCache(); cache != nil {
					cache.Invalidate(resolved)
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

// Invariant: headLen + tailLen must be < maxLines.
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
	skipped := tailStart - headLen
	if skipped > 0 {
		fmt.Fprintf(&b, "\n[... %d lines truncated ...]\n\n", skipped)
	}
	for i := range len(lines) - tailStart {
		b.WriteString(lines[tailStart+i])
		b.WriteByte('\n')
	}
	return b.String()
}

// toolPaths extracts file paths from tool arguments.
// For write/read: uses "path" key. For edit: parses the hashline patch DSL.

// confirmArgs adds a "path" entry for tools that store the file path in
// a non-standard argument name (e.g. edit uses "patch" not "path").
func confirmArgs(name string, args map[string]any) map[string]any {
	if name == "edit" {
		paths := ExtractPathsFromPatch(args["patch"])
		if len(paths) > 0 {
			out := make(map[string]any, len(args)+1)
			for k, v := range args {
				out[k] = v
			}
			out["path"] = paths[0]
			return out
		}
	}
	return args
}

func toolPaths(tc ToolCallItem) []string {
	if tc.Name == "edit" {
		return ExtractPathsFromPatch(tc.Args["patch"])
	}
	if p, ok := tc.Args["path"].(string); ok && p != "" {
		return []string{p}
	}
	return nil
}
