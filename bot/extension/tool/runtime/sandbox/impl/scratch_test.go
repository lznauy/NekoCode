package impl

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestScratchEnvironmentPreservesHomeAndDoesNotMutateCaller(t *testing.T) {
	env := []string{"HOME=/original", "GOPATH=/modules", "GOCACHE=/read-only", "TMPDIR=/tmp"}
	original := slices.Clone(env)
	dir := t.TempDir()
	got := commandEnvironment(env, Profile{ScratchDir: dir})
	for _, want := range []string{"HOME=/original", "GOPATH=/modules", "TMPDIR=/tmp", "NEKOCODE_SCRATCH=" + dir, "GOCACHE=" + filepath.Join(dir, "cache", "go-build")} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if !slices.Equal(env, original) {
		t.Fatal("caller environment mutated")
	}
}
