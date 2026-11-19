package impl

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed browser-check.mjs
var browserCheckSource string

// commandEnvironment changes only sandbox commands. Shell exports still take
// precedence; the user's HOME and module cache configuration are preserved.
func commandEnvironment(env []string, profile Profile) []string {
	directory := scratchAccessPath(profile)
	if directory == "" {
		return env
	}
	env = append([]string(nil), env...)
	env = setEnv(env, "NEKOCODE_SCRATCH", directory)
	// Supply the embedded helper afresh for each command across permission grants.
	env = setEnv(env, "NEKOCODE_BROWSER_CHECK", browserCheckSource)
	// Headless Chrome creates its temporary profile beneath the config root.
	env = setEnv(env, "CHROME_CONFIG_HOME", filepath.Join(directory, "chrome-config"))
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
