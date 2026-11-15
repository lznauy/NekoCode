//go:build linux
// +build linux

package impl

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeScratchSurvivesCommandsWithoutSharingTmp(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}
	ws, scratch := t.TempDir(), t.TempDir()
	profile := Profile{Workspace: ws, ScratchDir: scratch, WritePaths: []string{scratch}}
	if _, err := runNativeBash(t.Context(), `printf retained > "$NEKOCODE_SCRATCH/evidence"; mkdir -p "$GOCACHE"; printf cached > "$GOCACHE/entry"; printf transient > /tmp/command-marker`, profile, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	out, err := runNativeBash(t.Context(), `test ! -e /tmp/command-marker && cat "$NEKOCODE_SCRATCH/evidence" "$GOCACHE/entry"`, profile, 10*time.Second)
	if err != nil || out != "retainedcached" {
		t.Fatalf("state lost or /tmp shared: %q, %v", out, err)
	}
}

func TestNativeLoopbackProbe(t *testing.T) {
	if os.Getenv("NEKOCODE_NATIVE_LOOPBACK_PROBE") != "1" {
		t.Skip("sandbox helper only")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("loopback-ok")) }))
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 2 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "loopback-ok" {
		t.Fatalf("local HTTP failed: %q, %v", data, err)
	}
	if connection, err := net.DialTimeout("tcp4", "198.51.100.1:80", 200*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("isolated network unexpectedly reached outbound address")
	}
}

func TestNativeLoopbackSupportsHTTPWithoutOutboundNetwork(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}
	ws := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(ws, "loopback-probe")
	if err = os.WriteFile(probe, data, 0700); err != nil {
		t.Fatal(err)
	}
	command := "NEKOCODE_NATIVE_LOOPBACK_PROBE=1 '" + strings.ReplaceAll(probe, "'", "'\\''") + "' -test.run='^TestNativeLoopbackProbe$' -test.v"
	out, err := runNativeBash(t.Context(), command, Profile{Workspace: ws}, 10*time.Second)
	if err != nil || !strings.Contains(out, "--- PASS: TestNativeLoopbackProbe") {
		t.Fatalf("isolated local HTTP: %v\n%s", err, out)
	}
}

func TestRunNativeBash_SimpleEcho(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available (user namespaces not working)")
	}

	ws := t.TempDir()
	out, err := runNativeBash(t.Context(), "echo hello", Profile{Workspace: ws}, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected 'hello' in output, got: %q", out)
	}
}

func TestRunNativeBash_WorkspaceWrite(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}

	ws := t.TempDir()
	_, err := runNativeBash(t.Context(), "echo test > wsfile.txt", Profile{Workspace: ws}, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws, "wsfile.txt"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if !strings.Contains(string(data), "test") {
		t.Fatalf("file content mismatch: %q", string(data))
	}
}

func TestRunNativeBash_ReadOnlyWorkspace(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}

	ws := t.TempDir()
	_, err := runNativeBash(t.Context(), "echo test > wsfile.txt", Profile{Workspace: ws, Mode: ModeReadOnly}, 10*time.Second)
	if err == nil {
		t.Fatal("expected workspace write to fail in read-only mode")
	}
	if _, statErr := os.Stat(filepath.Join(ws, "wsfile.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("read-only workspace leaked write, stat err = %v", statErr)
	}
}

func TestRunNativeBash_TmpIsIsolated(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}

	ws := t.TempDir()
	// Write to /tmp inside sandbox, verify it does NOT appear on host /tmp.
	_, err := runNativeBash(t.Context(), "echo isolated > /tmp/nekocode_isolated_test", Profile{Workspace: ws}, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = os.Stat("/tmp/nekocode_isolated_test")
	if err == nil {
		t.Fatal("file in sandbox /tmp leaked to host")
		os.Remove("/tmp/nekocode_isolated_test")
	}
}

func TestRunNativeBash_CommandFailure(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}

	ws := t.TempDir()
	_, err := runNativeBash(t.Context(), "exit 42", Profile{Workspace: ws}, 10*time.Second)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}

func TestRunNativeBash_CachePathMounted(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}

	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	cacheDir := filepath.Join(home, ".npm")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}

	ws := t.TempDir()
	out, err := runNativeBash(t.Context(), "ls ~/.npm", Profile{
		Workspace:  ws,
		WritePaths: []string{cacheDir},
	}, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Output should not contain "No such file" — the mount worked.
	if strings.Contains(out, "No such file") {
		t.Fatalf("cache path not mounted: %q", out)
	}
}

// Verify source creation independently of host HOME permissions.
func TestRunNativeBash_WritePathCreated(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}
	testNativeWritePathCreation(t, t.TempDir())
}

// Also cover a source beneath a top-level bind that becomes read-only inside
// the sandbox. The host directory itself must be writable to honor WritePaths.
func TestRunNativeBash_WritePathCreatedUnderHome(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	base, err := os.MkdirTemp(home, ".nekocode_test_writepath_")
	if err != nil {
		if errors.Is(err, syscall.EROFS) || errors.Is(err, os.ErrPermission) {
			t.Skipf("host HOME is not writable: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	testNativeWritePathCreation(t, base)
}

func testNativeWritePathCreation(t *testing.T, base string) {
	t.Helper()
	for _, mode := range []string{"", "1"} {
		name := "visible"
		if mode != "" {
			name = "hidden"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("NEKOCODE_SANDBOX_HIDE_FS", mode)
			newDir := filepath.Join(base, name, "missing", "nested")
			ws := t.TempDir()
			file := filepath.Join(newDir, "file.txt")
			command := "echo data > '" + strings.ReplaceAll(file, "'", "'\"'\"'") + "'"
			_, err := runNativeBash(t.Context(), command, Profile{
				Workspace: ws, WritePaths: []string{newDir},
			}, 10*time.Second)
			if err != nil {
				t.Fatalf("write to non-existent WritePath failed: %v", err)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "data\n" {
				t.Fatalf("host WritePath did not receive data: %q, %v", data, err)
			}
		})
	}
}

func TestRunNativeBash_ReadOnlySystem(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}

	ws := t.TempDir()
	// Try to write to /usr — should fail because it's read-only.
	_, err := runNativeBash(t.Context(), "echo x > /usr/test_write", Profile{Workspace: ws}, 10*time.Second)
	if err == nil {
		t.Fatal("expected write to /usr to fail")
	}
}

func TestRunNativeBash_Timeout(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}

	ws := t.TempDir()
	_, err := runNativeBash(t.Context(), "sleep 30", Profile{Workspace: ws}, 2*time.Second)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunNativeBash_LargeOutput(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox not available")
	}

	ws := t.TempDir()
	// Generate output larger than maxOutputBytes (1MB).
	out, err := runNativeBash(t.Context(), "head -c 2000000 /dev/urandom | base64", Profile{Workspace: ws}, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) > maxOutputBytes+100 {
		t.Fatalf("output not truncated: got %d bytes", len(out))
	}
}
