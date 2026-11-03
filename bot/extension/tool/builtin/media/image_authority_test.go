package media

import (
	"context"
	"nekocode/bot/extension/tool/runtime/authority"
	"os"
	"path/filepath"
	"testing"
)

func TestImageOutputRejectsAuthorityDirectoryAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root, err := authority.Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(home, ".nekocode", "images")); err != nil {
		t.Skip(err)
	}
	if _, err := resolveOutputDir(context.Background(), nil); err == nil {
		t.Fatal("default image directory followed link into authority")
	}
}
func TestImageFileRejectsAuthorityTargetLink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	target, err := authority.StorePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("trusted"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "image.png")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if err := writeImageFile(link, func(f *os.File) error { _, err := f.Write([]byte("untrusted")); return err }); err == nil {
		t.Fatal("image write followed authority link")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "trusted" {
		t.Fatal("authority modified")
	}
}
func TestImageURLSuffixIsOnlyAnExtension(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"https://example.test/a.PNG?download=1", ".png"},
		{"https://example.test/a.jpeg#view", ".jpeg"},
		{"https://example.test/a.webp", ".webp"},
		{"https://example.test/a.png/subdir", ".jpg"},
		{"https://example.test/a.png%2fsubdir", ".jpg"},
		{"https://example.test/a.png/../../target", ".jpg"},
		{"https://example.test/a.png\\target", ".jpg"},
	} {
		if got := imageExtFromURL(tc.url); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.url, got, tc.want)
		}
	}
}
