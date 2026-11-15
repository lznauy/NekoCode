package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"nekocode/bot/extension/tool/runtime/sandbox"
	"nekocode/bot/extension/tool/runtime/workspace"
)

// A cache located in the writable workspace must not let a model turn the
// host-assigned scratch path into a grant for an unrelated external directory.
func TestScratchReplacementCannotGrantExternalWrite(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux sandbox required")
	}
	// Exercise Landlock's path-based grants independently of namespace mounts.
	t.Setenv("NEKOCODE_DISABLE_NATIVE_SANDBOX", "1")
	if !sandbox.IsAvailable() {
		t.Skip("Landlock sandbox unavailable")
	}
	directory, external := t.TempDir(), t.TempDir()
	cache := filepath.Join(directory, ".cache")
	t.Setenv("XDG_CACHE_HOME", cache)
	tool := &ShellTool{initialWait: time.Second}
	tool.SetSessionID("scratch-symlink-regression")
	// A hardened registry may report that the scratch path was replaced. The
	// test's temporary directory owns the replacement and cleans it up safely.
	t.Cleanup(func() { _ = tool.Shutdown() })
	ctx := workspace.WithManager(t.Context(), workspace.New(directory, nil))
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	baseline := filepath.Join(external, "baseline")
	out, err := tool.Execute(ctx, map[string]any{"command": "printf baseline > " + quote(baseline)})
	if err == nil && out.Process.State == "exited" && out.Process.ExitCode == 0 {
		t.Fatal("external directory was already writable before the scratch attack")
	}
	if _, err := os.Stat(baseline); !os.IsNotExist(err) {
		t.Fatalf("baseline external write was not blocked: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(cache, "nekocode", "shell", "actor-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("scratch directories=%v err=%v", matches, err)
	}
	original := matches[0]
	relative, err := filepath.Rel(filepath.Dir(original), external)
	if err != nil {
		t.Fatal(err)
	}
	attack := "mv -- " + quote(original) + " " + quote(original+".original") + " && ln -s -- " + quote(relative) + " " + quote(original)
	out, err = tool.Execute(ctx, map[string]any{"command": attack})
	if err != nil || out.Process.State != "exited" || !out.Process.ExitCodeKnown || out.Process.ExitCode != 0 {
		t.Fatalf("workspace-local scratch replacement failed: output=%s process=%+v err=%v", out.Render(), out.Process, err)
	}
	marker := filepath.Join(external, "external-marker")
	out, err = tool.Execute(ctx, map[string]any{"command": `printf escaped > "$NEKOCODE_SCRATCH/external-marker"`})
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("scratch replacement granted external write: marker=%s stat=%v process=%+v error=%v", marker, statErr, out.Process, err)
	}
}
