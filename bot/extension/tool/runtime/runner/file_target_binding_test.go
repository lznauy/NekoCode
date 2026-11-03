package runner

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"nekocode/bot/config"
	tools "nekocode/bot/extension/tool"
	readtool "nekocode/bot/extension/tool/builtin/filesystem/read"
	writetool "nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/builtin/media"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/protocol"
)

func TestApprovalRejectsFileTargetChangedWhileWaiting(t *testing.T) {
	for _, name := range []string{"read", "write", "image_gen"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			public, private := filepath.Join(root, "public"), filepath.Join(root, "private")
			for _, dir := range []string{public, private} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			// Probe symlink support before opening approval, including on Windows.
			probe := filepath.Join(root, "probe")
			if err := os.Symlink(private, probe); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := os.Remove(probe); err != nil {
				t.Fatal(err)
			}
			registry := tools.New(&readtool.ReadTool{}, &writetool.WriteTool{}, fakeTool{name: "image_gen"})
			registry.SetWorkspace(workspace.New(root, nil))
			e := NewExecutor(registry)
			e.SetProjectStore(root)
			e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{name + "(/private/**)"}, Ask: []string{name}}, root, root)
			args := map[string]any{"path": filepath.Join(public, "file"), "content": "new content", "startLine": float64(1), "endLine": float64(10)}
			if name == "read" {
				for _, dir := range []string{public, private} {
					if err := os.WriteFile(filepath.Join(dir, "file"), []byte(dir), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if name == "image_gen" {
				args = map[string]any{"output_dir": public}
			}
			approved := false
			e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
				approved = true
				if err := os.Rename(public, public+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(private, public); err != nil {
					t.Fatal(err)
				}
				return protocol.AllowOnce()
			})
			result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "changed-target", Name: name, Args: args}})[0]
			if !approved || !strings.Contains(result.Error, "file target changed") || len(result.Evidence.Executions) != 0 {
				t.Fatalf("changed target reached dispatch: approved=%t result=%+v", approved, result)
			}
			if name == "write" {
				if _, err := os.Stat(filepath.Join(private, "file")); !os.IsNotExist(err) {
					t.Fatalf("changed write target was touched: %v", err)
				}
			}
		})
	}
}

func TestBatchImageApprovalStillBindsEveryInput(t *testing.T) {
	for _, change := range []string{"content", "symlink"} {
		t.Run(change, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			first, second, other := filepath.Join(root, "first.png"), filepath.Join(root, "second.png"), filepath.Join(root, "other.png")
			writePNG := func(path string, c color.Color) {
				t.Helper()
				img := image.NewRGBA(image.Rect(0, 0, 1, 1))
				img.Set(0, 0, c)
				var data bytes.Buffer
				if err := png.Encode(&data, img); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			writePNG(first, color.RGBA{R: 255, A: 255})
			writePNG(second, color.RGBA{G: 255, A: 255})
			writePNG(other, color.RGBA{B: 255, A: 255})
			if change == "symlink" {
				probe := filepath.Join(root, "probe")
				if err := os.Symlink(other, probe); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				if err := os.Remove(probe); err != nil {
					t.Fatal(err)
				}
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			tool := media.NewImageUnderstandTool([]config.ImageUnderstandConfig{{Name: "vision", Model: "vision", BaseURL: server.URL}})
			registry := tools.New()
			registry.SetWorkspace(workspace.New(root, nil))
			registry.RegisterWithOptions(tool, tools.RegistrationOptions{PermissionPlan: tool.PermissionPlan})
			e := NewExecutor(registry)
			e.SetProjectStore(root)
			approved := false
			e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
				approved = true
				if change == "content" {
					writePNG(second, color.RGBA{B: 255, A: 255})
				} else {
					if err := os.Remove(second); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(other, second); err != nil {
						t.Fatal(err)
					}
				}
				return protocol.AllowOnce()
			})
			result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "batch-change", Name: "image_understand", Args: map[string]any{"paths": []string{first, second}}}})[0]
			if !approved || result.Error == "" || requests.Load() != 0 {
				t.Fatalf("changed batch image reached remote request: approved=%v requests=%d result=%+v", approved, requests.Load(), result)
			}
			if change == "content" && !strings.Contains(result.Error, "changed") {
				t.Fatalf("batch digest was not checked: %+v", result)
			}
		})
	}
}
