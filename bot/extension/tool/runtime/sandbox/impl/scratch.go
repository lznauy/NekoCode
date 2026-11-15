package impl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// commandEnvironment changes only sandbox commands. Shell exports still take
// precedence; the user's HOME and module cache configuration are preserved.
func commandEnvironment(env []string, profile Profile) []string {
	directory := scratchAccessPath(profile)
	if directory == "" {
		return env
	}
	env = append([]string(nil), env...)
	env = setEnv(env, "NEKOCODE_SCRATCH", directory)
	env = setEnv(env, "XDG_CACHE_HOME", filepath.Join(directory, "cache"))
	return setEnv(env, "GOCACHE", filepath.Join(directory, "cache", "go-build"))
}

// Landlock and its descendants use the host's pinned descriptor, never the
// original replaceable directory name. Native children use a private mount.
func scratchAccessPath(profile Profile) string {
	if profile.ScratchFile != nil {
		return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), profile.ScratchFile.Fd())
	}
	return profile.ScratchDir
}

// setEnv replaces (or appends) the value of env key k in the KEY=VAL list.
func setEnv(env []string, k, v string) []string {
	prefix := k + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + v
			return env
		}
	}
	return append(env, prefix+v)
}
