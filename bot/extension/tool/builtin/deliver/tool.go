// Package deliver provides the deliver_file tool: the model pushes a
// workspace file to the user's IM chats through a configured connector.
package deliver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/bot/extension/tool/runtime/workspace"
)

// Deliverer is the injection point for the file-delivery backend. It is
// satisfied by the runtime assembly so the tool stays ignorant of connector
// internals (dependency direction: standard assembles bot+interaction).
type Deliverer interface {
	// DeliverConnectorFile sends path to the named connector's paired
	// chats, or to every running file-capable connector when connector is
	// empty. It returns the connector names that delivered the file.
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
	return "Send a workspace file (e.g. generated archives, reports, datasets) to the user's chat via a configured IM connector (telegram or feishu). Only files inside the current workspace are allowed. Use it when the user asks you to send/give/transfer a file."
}

func (t *DeliverFileTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "Path to the workspace file to deliver"},
		{Name: "connector", Type: "string", Required: false, Description: "Target connector name (e.g. \"telegram\", \"feishu\"). Omit to deliver to every running file-capable connector."},
	}
}

// maxDeliverFileBytes caps one delivered file. IM platforms hard-reject
// oversized uploads (Telegram documents cap at 50 MB); failing fast in the
// tool gives the model a clear, actionable error instead of a per-connector
// platform rejection, and bounds the multipart memory spike on the send path.
const maxDeliverFileBytes = 50 << 20

func (t *DeliverFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path := strings.TrimSpace(toolutil.OptStringArg(args, "path", ""))
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	// Resolve relative paths against the workspace root first — the readable
	// roots check below only understands absolute paths.
	if !filepath.IsAbs(path) {
		if manager, ok := workspace.FromContext(ctx); ok {
			if roots := manager.Snapshot(); len(roots) > 0 {
				path = filepath.Join(roots[0].Path, path)
			}
		}
	}
	// Workspace gate: only files under the session workspace leave the machine.
	safePath, err := toolutil.ValidatePathReadableContext(ctx, path)
	if err != nil {
		return "", fmt.Errorf("file must be inside the current workspace: %w", err)
	}
	info, err := os.Stat(safePath)
	if err != nil {
		return "", fmt.Errorf("read file size: %w", err)
	}
	if info.Size() > maxDeliverFileBytes {
		return "", fmt.Errorf("file is %d MiB, above the %d MiB delivery limit (IM platform constraints); give the user the local path instead or split the file",
			info.Size()>>20, maxDeliverFileBytes>>20)
	}

	connector := strings.TrimSpace(toolutil.OptStringArg(args, "connector", ""))
	sent, err := t.deliverer.DeliverConnectorFile(ctx, connector, safePath)
	if err != nil {
		if len(sent) > 0 {
			return fmt.Sprintf("Delivered via %s, but other targets failed: %v", strings.Join(sent, ", "), err), nil
		}
		return "", fmt.Errorf("file delivery failed: %w", err)
	}
	return fmt.Sprintf("File delivered to %s: %s", strings.Join(sent, ", "), safePath), nil
}
