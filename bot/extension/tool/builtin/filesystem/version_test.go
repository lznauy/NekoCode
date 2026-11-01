package filesystem_test

import (
	"nekocode/bot/extension/tool/builtin/filesystem/edit"
	"nekocode/bot/extension/tool/builtin/filesystem/read"
	textutil "nekocode/util/text"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
	"nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/runtime/execution"
)

func TestManagedWriteRequiresDeliveredRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := execution.WithExecutionState(testutil.Context(root), execution.NewExecutionState())
	if _, err := (&write.WriteTool{}).Execute(ctx, map[string]any{"path": path, "content": "overwrite"}); err == nil {
		t.Fatal("managed write overwrote an existing file without a delivered read")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Fatalf("rejected write changed original: %q, %v", data, err)
	}
}

func TestManagedReadWriteVersion(t *testing.T) {
	for _, operation := range []string{"write", "edit"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "file.txt")
			if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
				t.Fatal(err)
			}
			state := execution.NewExecutionState()
			ctx := execution.WithExecutionState(testutil.Context(root), state)
			mutate := func() error {
				if operation == "write" {
					_, err := (&write.WriteTool{}).Execute(ctx, map[string]any{"path": path, "content": "after"})
					return err
				}
				_, err := (&edit.EditTool{}).Execute(ctx, map[string]any{"path": path, "oldString": "before", "newString": "after"})
				return err
			}
			readFile := func() {
				state.Reads.Observe(nil)
				out, err := (&read.ReadTool{}).Execute(ctx, map[string]any{"path": path, "startLine": float64(1), "endLine": float64(10)})
				if err != nil {
					t.Fatal(err)
				}
				if mutate() == nil {
					t.Fatal("undelivered read authorized mutation")
				}
				state.Reads.Observe(map[string]string{out.ReadID: textutil.NormalizeTerminalOutput(out.Render())})
			}
			readFile()
			info, _ := os.Stat(path)
			if err := os.WriteFile(path, []byte("BEFORE"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if mutate() == nil {
				t.Fatal("same size/mtime external change was overwritten")
			}
			if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
				t.Fatal(err)
			}
			readFile()
			if err := mutate(); err != nil {
				t.Fatal(err)
			}
			if err := state.Reads.Check(path, []byte("after")); err == nil {
				t.Fatal("mutation retained read authority")
			}
		})
	}
}

func TestReadRejectsStaleCacheWithUnchangedMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := execution.WithExecutionState(testutil.Context(root), execution.NewExecutionState())
	args := map[string]any{"path": path, "startLine": float64(1), "endLine": float64(10)}
	if _, err := (&read.ReadTool{}).Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if err := os.WriteFile(path, []byte("AFTER!"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	out, err := (&read.ReadTool{}).Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "1:AFTER!") || strings.Contains(out.Text, "1:before") {
		t.Fatalf("read returned stale preview: %s", out.Text)
	}
}

func TestConcurrentActorsCannotBothWriteReadVersion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, content := range []string{"actor-a", "actor-b"} {
		state := execution.NewExecutionState()
		ctx := execution.WithExecutionState(testutil.Context(root), state)
		out, err := (&read.ReadTool{}).Execute(ctx, map[string]any{"path": path, "startLine": float64(1), "endLine": float64(10)})
		if err != nil {
			t.Fatal(err)
		}
		state.Reads.Observe(map[string]string{out.ReadID: textutil.NormalizeTerminalOutput(out.Render())})
		go func() {
			<-start
			_, err := (&write.WriteTool{}).Execute(ctx, map[string]any{"path": path, "content": content})
			results <- err
		}()
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent mutations succeeded %d times", successes)
	}
}

func TestManagedWriteCreatesNewFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new", "file.txt")
	ctx := execution.WithExecutionState(testutil.Context(root), execution.NewExecutionState())
	if _, err := (&write.WriteTool{}).Execute(ctx, map[string]any{"path": path, "content": "created"}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "created" {
		t.Fatalf("created file = %q, %v", data, err)
	}
}
