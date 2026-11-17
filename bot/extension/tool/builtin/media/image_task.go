package media

import (
	"context"
	"crypto/rand"
	"fmt"
	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/protocol"
)

type imageTask struct {
	TaskID      string
	Config      config.ImageGenConfig
	OutputDir   string
	ExplicitDir bool
	Scope       string
}

func (t *ImageGenTool) rememberImage(task imageTask) string {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	if t.pending == nil {
		t.pending = map[string]imageTask{}
	}
	if len(t.pendingOrder) >= 128 {
		delete(t.pending, t.pendingOrder[0])
		t.pendingOrder = t.pendingOrder[1:]
	}
	id := "image_" + rand.Text()
	t.pending[id] = task
	t.pendingOrder = append(t.pendingOrder, id)
	return id
}

// ResumeOutputDir reports the output directory saved with a pending resume
// receipt, so callers can re-evaluate permission rules against the exact
// destination the recovery call would download to.
func (t *ImageGenTool) ResumeOutputDir(id string) (dir string, explicit bool) {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	task, ok := t.pending[id]
	if !ok {
		return "", false
	}
	return task.OutputDir, task.ExplicitDir
}

func (t *ImageGenTool) resumeImage(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	id, err := toolutil.RequireStringArg(args, "resume_id")
	if err != nil {
		return core.ToolOutput{}, err
	}
	for key := range args {
		if key != "resume_id" {
			return core.ToolOutput{}, fmt.Errorf("resume_id must be used alone; no new generation was submitted")
		}
	}
	t.pendingMu.Lock()
	task, ok := t.pending[id]
	t.pendingMu.Unlock()
	if !ok || task.Scope != imageScope(ctx) {
		return core.ToolOutput{}, fmt.Errorf("unknown or expired image resume_id; no task was submitted. Check the saved task ID before deciding whether another generation is needed")
	}
	output, err := t.collectImage(ctx, id, task)
	return core.ToolOutput{Text: output}, err
}
func (t *ImageGenTool) collectImage(ctx context.Context, id string, task imageTask) (string, error) {
	cfg := task.Config
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://visual.volcengineapi.com"
	}
	model := cfg.Model
	if model == "" {
		model = "jimeng_t2i_v31"
	}
	receipt := fmt.Sprintf("Generation already submitted: task_id=%s\nRecovery call: {\"resume_id\":%q}\n", task.TaskID, id)
	signer := NewSigner(cfg.APIKey, cfg.SecretKey, "cn-north-1", "cv")
	result, err := t.jimengPoll(ctx, signer, baseURL, map[string]any{"req_key": model, "task_id": task.TaskID, "req_json": `{"return_url":true}`})
	if err != nil {
		return receipt, fmt.Errorf("query accepted generation: %w; use the recovery call, do not resubmit the prompt", err)
	}
	// Recheck current workspace authority, including on resumed downloads.
	dirArgs := map[string]any{}
	if task.ExplicitDir {
		dirArgs["output_dir"] = task.OutputDir
	}
	dir, err := resolveOutputDir(ctx, dirArgs)
	if err != nil {
		return receipt, err
	}
	if dir != task.OutputDir {
		return receipt, fmt.Errorf("saved image output directory changed; no images were downloaded")
	}
	var output string
	if len(result.ImageURLs) > 0 {
		output, err = t.downloadImages(ctx, result.ImageURLs, dir)
	} else if len(result.BinaryDataBase64) > 0 {
		output, err = t.saveBase64Images(result.BinaryDataBase64, dir)
	} else {
		err = fmt.Errorf("completed generation returned no images")
	}
	if err != nil {
		return receipt + output, err
	}
	return output, nil
}

func imageScope(ctx context.Context) string {
	identity := protocol.ExecutionFromContext(ctx)
	scope := identity.TaskID
	if manager, ok := workspace.FromContext(ctx); ok {
		scope += "\x00" + manager.Primary() + "\x00" + manager.SessionID()
	}
	return scope
}
