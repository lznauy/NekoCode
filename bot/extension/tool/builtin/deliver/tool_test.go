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
	content   []byte
	hook      func(string) error
	sent      []string
	err       error
}

func (d *fakeDeliverer) DeliverConnectorFile(_ context.Context, connector, path string) ([]string, error) {
	d.connector, d.path = connector, path
	var err error
	d.content, err = os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if d.hook != nil {
		if err := d.hook(path); err != nil {
			return nil, err
		}
	}
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
	if d.path == path || filepath.Base(d.path) != "report.zip" || string(d.content) != "zip" {
		t.Fatalf("invalid upload snapshot: path=%q content=%q", d.path, d.content)
	}
	if _, err := os.Stat(d.path); !os.IsNotExist(err) {
		t.Fatalf("snapshot not cleaned up: %v", err)
	}
	if !strings.Contains(out.Render(), "telegram") {
		t.Fatalf("output = %q", out.Render())
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

	// Partial success preserves both successful targets and failure status.
	tool = NewTool(&fakeDeliverer{sent: []string{"telegram"}, err: errors.New("feishu: boom")})
	out, err := tool.Execute(ctx, map[string]any{"path": "a.txt"})
	if err == nil {
		t.Fatal("partial failure was reported as complete success")
	}
	if !strings.Contains(out.Render(), "telegram") || !strings.Contains(out.Render(), "feishu") {
		t.Fatalf("partial result = %q", out.Render())
	}
}

func TestRelativePathUsesPrimaryAndSnapshotSurvivesSourceChanges(t *testing.T) {
	root := t.TempDir()
	primary, reference := filepath.Join(root, "z-project"), filepath.Join(root, "a-reference")
	for _, dir := range []string{primary, reference} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte(filepath.Base(dir)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := workspace.WithManager(context.Background(), workspace.New(primary, []workspace.Root{{Path: reference, Access: workspace.AccessReadOnly}}))
	d := &fakeDeliverer{sent: []string{"telegram"}}
	d.hook = func(snapshot string) error {
		if err := os.WriteFile(filepath.Join(primary, "report.txt"), []byte("changed"), 0600); err != nil {
			return err
		}
		data, err := os.ReadFile(snapshot)
		if string(data) != "z-project" {
			t.Errorf("upload changed with source: %q", data)
		}
		return err
	}
	if _, err := NewTool(d).Execute(ctx, map[string]any{"path": "report.txt"}); err != nil {
		t.Fatal(err)
	}
	if string(d.content) != "z-project" {
		t.Fatalf("selected wrong root: %q", d.content)
	}
	if _, err := os.Stat(d.path); !os.IsNotExist(err) {
		t.Fatalf("snapshot leaked: %v", err)
	}
}

func TestDeliverySnapshotCleanedOnFailure(t *testing.T) {
	ctx, dir := wsContext(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &fakeDeliverer{err: errors.New("upload failed")}
	if _, err := NewTool(d).Execute(ctx, map[string]any{"path": "a.txt"}); err == nil {
		t.Fatal("missing failure")
	}
	if _, err := os.Stat(d.path); !os.IsNotExist(err) {
		t.Fatalf("failed snapshot leaked: %v", err)
	}
}
