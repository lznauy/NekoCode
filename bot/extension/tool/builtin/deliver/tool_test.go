package deliver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/workspace"
)

type fakeDeliverer struct {
	connector string
	path      string
	sent      []string
	err       error
}

func (d *fakeDeliverer) DeliverConnectorFile(_ context.Context, connector, path string) ([]string, error) {
	d.connector, d.path = connector, path
	return d.sent, d.err
}

func wsContext(t *testing.T) (context.Context, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return workspace.WithManager(context.Background(), workspace.New(dir, nil)), dir
}

func TestExecuteDeliversWorkspaceFile(t *testing.T) {
	ctx, dir := wsContext(t)
	path := filepath.Join(dir, "sub", "report.zip")
	if err := os.WriteFile(path, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &fakeDeliverer{sent: []string{"telegram"}}
	tool := NewTool(d)

	out, err := tool.Execute(ctx, map[string]any{"path": "sub/report.zip", "connector": "telegram"})
	if err != nil {
		t.Fatal(err)
	}
	if d.connector != "telegram" {
		t.Fatalf("connector = %q", d.connector)
	}
	// Relative paths resolve against the workspace root.
	if d.path != path {
		t.Fatalf("path = %q, want %q", d.path, path)
	}
	if !strings.Contains(out, "telegram") {
		t.Fatalf("output = %q", out)
	}
}

func TestExecuteRejectsPathOutsideWorkspace(t *testing.T) {
	ctx, _ := wsContext(t)
	tool := NewTool(&fakeDeliverer{})

	if _, err := tool.Execute(ctx, map[string]any{"path": "/etc/passwd"}); err == nil {
		t.Fatal("outside-workspace path must be rejected")
	}
	if _, err := tool.Execute(ctx, map[string]any{"path": ""}); err == nil {
		t.Fatal("empty path must be rejected")
	}
}

// TestExecuteRejectsOversizedFile guards the platform size cap: files above
// the IM delivery limit must fail in the tool with an actionable error
// (including the local path hint), never reaching the upload path.
func TestExecuteRejectsOversizedFile(t *testing.T) {
	ctx, dir := wsContext(t)
	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, maxDeliverFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewTool(&fakeDeliverer{sent: []string{"telegram"}})

	_, err := tool.Execute(ctx, map[string]any{"path": "big.bin"})
	if err == nil {
		t.Fatal("oversized file must be rejected")
	}
	if !strings.Contains(err.Error(), "delivery limit") || !strings.Contains(err.Error(), "local path") {
		t.Fatalf("err = %v, want size-limit error with path hint", err)
	}

	// Exactly at the cap passes (50 MiB is the platform's inclusive limit).
	atCap := filepath.Join(dir, "cap.bin")
	if err := os.WriteFile(atCap, make([]byte, maxDeliverFileBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, map[string]any{"path": "cap.bin"}); err != nil {
		t.Fatalf("file at the cap must still deliver: %v", err)
	}
}

func TestExecuteSurfacesDeliveryFailure(t *testing.T) {
	ctx, dir := wsContext(t)
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewTool(&fakeDeliverer{err: errors.New("no running connector supports file delivery")})

	_, err := tool.Execute(ctx, map[string]any{"path": "a.txt"})
	if err == nil || !strings.Contains(err.Error(), "file delivery failed") {
		t.Fatalf("err = %v, want delivery failure", err)
	}

	// Partial success (some connectors delivered) is reported, not an error.
	tool = NewTool(&fakeDeliverer{sent: []string{"telegram"}, err: errors.New("feishu: boom")})
	out, err := tool.Execute(ctx, map[string]any{"path": "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "telegram") || !strings.Contains(out, "feishu") {
		t.Fatalf("partial result = %q", out)
	}
}
