// Package deliver provides the deliver_file tool: the model pushes a
// workspace file to the user's IM chats through a configured connector.
package deliver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/bot/extension/tool/runtime/workspace"
	utilfs "nekocode/util/fs"
)

// Deliverer is the injection point for the file-delivery backend. It is
// satisfied by the runtime assembly so the tool stays ignorant of connector
// internals (dependency direction: standard assembles bot+interaction).
type Deliverer interface {
	// DeliverConnectorFile sends path to the named connector's paired
	// chats. An empty connector routes to the current request source; '*'
	// explicitly broadcasts to every running file-capable connector. It returns
	// the connector names that delivered the file.
	DeliverConnectorFile(ctx context.Context, connector, path string) ([]string, error)
}

type DeliverFileTool struct {
	toolutil.SequentialSafeTool
	deliverer Deliverer
}

func NewTool(deliverer Deliverer) *DeliverFileTool {
	return &DeliverFileTool{deliverer: deliverer}
}

func (t *DeliverFileTool) Name() string { return "deliver_file" }

func (t *DeliverFileTool) Description() string {
	return "Send a workspace file (e.g. generated archives, reports, datasets) to the user's chat via a configured IM connector (telegram or feishu). Only regular files in authorized readable workspace roots are allowed. Relative paths use the project root. Use it when the user asks you to send/give/transfer a file."
}

func (t *DeliverFileTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "Path to the workspace file to deliver"},
		{Name: "connector", Type: "string", Required: false, Description: "Target connector name (e.g. \"telegram\", \"feishu\"). Omit to reply to the current request source; local requests must specify a connector. Use \"*\" only when the user explicitly requests broadcast."},
	}
}

// maxDeliverFileBytes bounds the snapshot and subsequent multipart upload.
// A connector or remote API may impose a lower limit.
const maxDeliverFileBytes = 50 << 20

func (t *DeliverFileTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	path := strings.TrimSpace(toolutil.OptStringArg(args, "path", ""))
	if path == "" {
		return core.ToolOutput{}, fmt.Errorf("path is required")
	}
	if t.deliverer == nil {
		return core.ToolOutput{}, fmt.Errorf("file delivery backend unavailable")
	}
	manager, ok := workspace.FromContext(ctx)
	if !ok {
		return core.ToolOutput{}, fmt.Errorf("file delivery requires a workspace")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(manager.Primary(), path)
	}
	safePath, root, allowed, err := manager.CheckRead(path)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if !allowed {
		return core.ToolOutput{}, fmt.Errorf("file must be inside an authorized readable workspace")
	}
	relative, err := filepath.Rel(root.Path, safePath)
	if err != nil {
		return core.ToolOutput{}, err
	}
	data, err := utilfs.ReadRegularFileInRoot(ctx, root.Path, relative, maxDeliverFileBytes)
	if err != nil {
		if errors.Is(err, utilfs.ErrFileTooLarge) {
			return core.ToolOutput{}, fmt.Errorf("file exceeds the %d MiB delivery limit; give the user the local path instead or split the file", maxDeliverFileBytes>>20)
		}
		return core.ToolOutput{}, fmt.Errorf("read delivery file: %w", err)
	}
	// Connectors read this private snapshot, never reopening a mutable source.
	dir, err := os.MkdirTemp("", "nekocode-delivery-*")
	if err != nil {
		return core.ToolOutput{}, err
	}
	defer os.RemoveAll(dir)
	snapshot := filepath.Join(dir, filepath.Base(safePath))
	if err := os.WriteFile(snapshot, data, 0600); err != nil {
		return core.ToolOutput{}, err
	}
	if err := ctx.Err(); err != nil {
		return core.ToolOutput{}, err
	}

	connector := strings.TrimSpace(toolutil.OptStringArg(args, "connector", ""))
	sent, err := t.deliverer.DeliverConnectorFile(ctx, connector, snapshot)
	if err != nil {
		if len(sent) > 0 {
			return core.ToolOutput{Text: fmt.Sprintf("Delivered via %s, but other targets failed: %v", strings.Join(sent, ", "), err)}, err
		}
		return core.ToolOutput{}, fmt.Errorf("file delivery failed: %w", err)
	}
	if len(sent) == 0 {
		return core.ToolOutput{}, fmt.Errorf("file delivery returned no successful targets")
	}
	return core.ToolOutput{Text: fmt.Sprintf("File delivered to %s: %s", strings.Join(sent, ", "), safePath)}, nil
}
