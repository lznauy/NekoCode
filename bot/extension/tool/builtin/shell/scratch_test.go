package shell

import (
	"nekocode/bot/extension/tool/runtime/sandbox"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestScratchReusedWithinActorAndIsolatedAcrossOwnersAndWorkspaces(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	r := newTaskRegistry()
	t.Cleanup(func() { _ = r.shutdown() })
	workspace := t.TempDir()
	one := startRequest{owner: "a", directory: workspace}
	if err := r.prepareScratch(&one); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(one.profile.ScratchDir, "evidence.txt")
	if err := os.WriteFile(file, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	again := startRequest{owner: "a", directory: workspace}
	if err := r.prepareScratch(&again); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(again.profile.ScratchDir, "evidence.txt")); err != nil || string(data) != "retained" {
		t.Fatalf("command state lost: %q, %v", data, err)
	}
	for _, next := range []startRequest{{owner: "b", directory: workspace}, {owner: "a", directory: t.TempDir()}} {
		if err := r.prepareScratch(&next); err != nil {
			t.Fatal(err)
		}
		if next.profile.ScratchDir == one.profile.ScratchDir {
			t.Fatal("scratch crossed actor/workspace boundary")
		}
	}
	if err := r.shutdown(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("scratch retained after shutdown: %v", err)
	}
}

func TestSharedShellSeparatesSubagentScratchFromParentAndSiblings(t *testing.T) {
	if runtime.GOOS != "linux" || !sandbox.IsAvailable() {
		t.Skip("Linux sandbox required")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	tool := &ShellTool{initialWait: time.Second}
	tool.SetSessionID("shared-session")
	cleanupShellTool(t, tool)
	ctx := workspace.WithManager(t.Context(), workspace.New(t.TempDir(), nil))
	paths := map[string]string{}
	for _, actor := range []string{"", "child-a", "child-b", "child-a"} {
		out, err := tool.Execute(taskbridge.WithTaskID(ctx, actor), map[string]any{"command": `printf '%s' "$NEKOCODE_SCRATCH"`})
		if err != nil || out.Process.State != "exited" || out.Process.ExitCode != 0 {
			t.Fatalf("actor %q failed: %+v, %v", actor, out, err)
		}
		path := strings.TrimSpace(out.Render())
		if path == "" {
			t.Fatal("no scratch environment")
		}
		if prior := paths[actor]; prior != "" && prior != path {
			t.Fatal("same child lost scratch")
		}
		for other, prior := range paths {
			if other != actor && prior == path {
				t.Fatalf("actors %q and %q share scratch", actor, other)
			}
		}
		paths[actor] = path
	}
}
