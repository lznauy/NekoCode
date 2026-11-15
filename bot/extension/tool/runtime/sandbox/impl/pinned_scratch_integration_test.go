//go:build linux

package impl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPinnedScratchProbe(t *testing.T) {
	hostPath := os.Getenv("NEKOCODE_PINNED_HOST_PATH")
	if hostPath == "" {
		t.Skip("sandbox helper only")
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		link, _ := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if strings.Contains(link, hostPath) {
			t.Fatalf("old host directory descriptor leaked: %s -> %s", entry.Name(), link)
		}
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("NEKOCODE_SCRATCH"), "marker"), []byte("pinned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostPath, "host-escape"), []byte("forbidden"), 0600); err == nil {
		t.Fatal("original host cache path became writable")
	}
}

func TestNativePinnedScratchDoesNotLeakHostDescriptors(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox unavailable")
	}
	for _, domains := range [][]string{nil, {}} {
		name := "ordinary exec"
		if domains != nil {
			name = "restricted supervisor"
		}
		t.Run(name, func(t *testing.T) {
			workspace, scratch := t.TempDir(), t.TempDir()
			file, err := os.Open(scratch)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			binary, err := os.ReadFile(self)
			if err != nil {
				t.Fatal(err)
			}
			probe := filepath.Join(workspace, "fd-probe")
			if err = os.WriteFile(probe, binary, 0700); err != nil {
				t.Fatal(err)
			}
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			command := "NEKOCODE_PINNED_HOST_PATH=" + quote(scratch) + " " + quote(probe) + " -test.run='^TestPinnedScratchProbe$' -test.v"
			out, err := runNativeBash(t.Context(), command, Profile{Workspace: workspace, ScratchDir: scratch, ScratchFile: file, OutboundDomains: domains}, 10*time.Second)
			if err != nil || !strings.Contains(out, "--- PASS: TestPinnedScratchProbe") {
				t.Fatalf("pinned scratch exec: %v\n%s", err, out)
			}
			if data, err := os.ReadFile(filepath.Join(scratch, "marker")); err != nil || string(data) != "pinned" {
				t.Fatalf("wrong mount: %q %v", data, err)
			}
		})
	}
}

func TestNativePinnedScratchRejectsReplacedSource(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox unavailable")
	}
	workspace, scratch, external := t.TempDir(), t.TempDir(), t.TempDir()
	file, err := os.Open(scratch)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = os.Rename(scratch, scratch+".original"); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(scratch + ".original")
	if err = os.Symlink(external, scratch); err != nil {
		t.Fatal(err)
	}
	out, err := runNativeBash(t.Context(), `printf escaped > "$NEKOCODE_SCRATCH/marker"`, Profile{Workspace: workspace, ScratchDir: scratch, ScratchFile: file}, 10*time.Second)
	if err == nil || !strings.Contains(out+err.Error(), "scratch mount source was replaced") {
		t.Fatalf("replacement accepted: %q, %v", out, err)
	}
	if _, err = os.Stat(filepath.Join(external, "marker")); !os.IsNotExist(err) {
		t.Fatalf("external directory written: %v", err)
	}
}

func TestNativePinnedScratchAllowsExistingAuthorizedMountDirectory(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox unavailable")
	}
	workspace := t.TempDir()
	scratch, err := os.MkdirTemp(t.TempDir(), "actor-")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(scratch)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// An independently authorized host path can already occupy the reserved
	// namespace target. Scratch must still bind its pinned actor directory.
	alias := filepath.Join("/tmp", ".nekocode-"+filepath.Base(scratch))
	if err := os.Mkdir(alias, 0700); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(alias)
	if err := os.WriteFile(filepath.Join(scratch, "marker"), []byte("actor-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alias, "marker"), []byte("host-content"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runNativeBash(t.Context(), `cat "$NEKOCODE_SCRATCH/marker"`, Profile{Workspace: workspace, ScratchDir: scratch, ScratchFile: file, WritePaths: []string{alias}}, 10*time.Second)
	if err != nil || out != "actor-content" {
		t.Fatalf("pinned scratch not mounted: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(filepath.Join(alias, "marker")); err != nil || string(data) != "host-content" {
		t.Fatalf("host alias changed: %q %v", data, err)
	}
}

func TestPinnedScratchRejectsNonDirectoryAndSymlinkTargets(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "symlink"}[symlink], func(t *testing.T) {
			root, source := t.TempDir(), t.TempDir()
			target := filepath.Join(root, "tmp", "actor-target")
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			if symlink {
				if err := os.Symlink(t.TempDir(), target); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(target, []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			fd, err := unix.Dup(int(file.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			// mountPinnedScratch owns and closes the inherited duplicate.
			err = mountPinnedScratch(root, Profile{ScratchFD: fd, ScratchSource: source, ScratchDir: "/tmp/actor-target"})
			if err == nil || !strings.Contains(err.Error(), "open scratch mount target") {
				t.Fatalf("unsafe target accepted: %v", err)
			}
		})
	}
}
