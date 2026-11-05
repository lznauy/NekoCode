package connect

import (
	"fmt"
	controlruntime "nekocode/runtime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalImagePaths(t *testing.T) {
	dir := t.TempDir()
	exist := func(name string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	chart := exist("chart.png")
	photo := exist("photo.jpg")
	missing := filepath.Join(dir, "gone.png")

	text := "图表保存在 " + chart + "，照片见 " + photo + "。缺失的 " + missing + " 和目录 " + dir + " 应被忽略。"
	got := LocalImagePaths(text)
	if len(got) != 2 || got[0] != chart || got[1] != photo {
		t.Fatalf("LocalImagePaths = %v, want [%s %s]", got, chart, photo)
	}

	// Relative paths and URLs never match.
	if p := LocalImagePaths("see ./out.png and https://x.test/a.png"); p != nil {
		t.Fatalf("relative/url paths should not match: %v", p)
	}
	// Duplicates collapse.
	dup := "one " + chart + " two " + chart
	if p := LocalImagePaths(dup); len(p) != 1 || p[0] != chart {
		t.Fatalf("duplicate handling = %v", p)
	}
	// Cap at maxOutboundImages.
	var many []string
	for i := 0; i < maxOutboundImages+2; i++ {
		many = append(many, exist(strings.Repeat("a", i)+"x.png"))
	}
	if p := LocalImagePaths(strings.Join(many, " ")); len(p) != maxOutboundImages {
		t.Fatalf("cap = %d, want %d", len(p), maxOutboundImages)
	}
}

func TestTemporaryImageRelease(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint("replace=", replace), func(t *testing.T) {
			path, err := DownloadToTemp("nekocode-owned-test-*.png", func(path string) error { return os.WriteFile(path, []byte("original"), 0600) })
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(path) })
			release := TemporaryImageRelease(path)
			if release == nil {
				t.Fatal("missing ownership callback")
			}
			if replace {
				// Keep the original inode allocated so this checks a true replacement.
				old := path + ".old"
				if err := os.Rename(path, old); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Remove(old) })
				if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			release()
			release()
			_, err = os.Stat(path)
			if replace && err != nil {
				t.Fatalf("replacement removed: %v", err)
			}
			if !replace && !os.IsNotExist(err) {
				t.Fatalf("owned temporary file not removed: %v", err)
			}
		})
	}
}

func TestReleaseImagesOnlyCleansOwnedDownloads(t *testing.T) {
	owned := filepath.Join(t.TempDir(), "owned.png")
	caller := filepath.Join(t.TempDir(), "caller.png")
	for _, path := range []string{owned, caller} {
		if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	images := []controlruntime.ImageAttachment{{Path: owned, Release: TemporaryImageRelease(owned)}, {Path: caller}}
	ReleaseImages(images)
	ReleaseImages(images)
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("owned download retained: %v", err)
	}
	if _, err := os.Stat(caller); err != nil {
		t.Fatalf("caller file removed: %v", err)
	}
}
