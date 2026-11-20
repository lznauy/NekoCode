package fs

import (
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInvalidHomeCannotResolveIntoWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("tests Unix HOME resolution")
	}
	for _, home := range []string{"", "relative"} {
		t.Setenv("HOME", home)
		if _, err := UserHomeDir(); err == nil {
			t.Fatal("accepted invalid HOME", home)
		}
		account, err := user.Current()
		if err == nil && filepath.IsAbs(account.HomeDir) && NekocodeHome() != filepath.Join(account.HomeDir, ".nekocode") {
			t.Fatal("unexpected state directory", NekocodeHome())
		}
	}
}
