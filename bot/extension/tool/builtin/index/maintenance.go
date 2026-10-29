package index

import (
	"context"
	"fmt"

	indexcore "nekocode/bot/extension/tool/builtin/index/core"
	"nekocode/bot/extension/tool/runtime/workspace"
)

// Maintain shares the tool's workspace and lifetime. Exclusive access prevents
// rebuild or close from invalidating an in-flight query.
func (t *IndexTool) Maintain(ctx context.Context, action string) (string, error) {
	t.opMu.Lock()
	defer t.opMu.Unlock()
	if action != "status" && action != "rebuild" {
		return "", fmt.Errorf("usage: /index [status|rebuild]")
	}
	cwd, err := workspace.WorkingDirectory(ctx)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	if t.closed || (t.root != "" && t.root != cwd) {
		t.mu.Unlock()
		return "", fmt.Errorf("index is closed or belongs to another workspace")
	}
	t.mu.Unlock()
	if action == "status" {
		mgr, err := t.ensureInit(ctx)
		if err != nil {
			return "", fmt.Errorf("index unavailable: %w; run /index rebuild", err)
		}
		return mgr.StatusReport()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mgr != nil {
		previous := t.mgr
		t.mgr = nil
		if err := previous.Close(); err != nil {
			t.mgrErr = fmt.Errorf("close index before rebuild: %w", err)
			return "", t.mgrErr
		}
	}
	t.root = cwd
	manager, backup, err := indexcore.RebuildManager(cwd)
	t.mgr = manager
	t.mgrErr = err
	if err != nil {
		return "", fmt.Errorf("rebuild failed: %w (backup: %s)", err, backup)
	}
	report, err := t.mgr.StatusReport()
	if err != nil {
		return "", err
	}
	if backup != "" {
		report += "\nPrevious database backup: " + backup
	}
	return "Index rebuilt.\n" + report, nil
}
