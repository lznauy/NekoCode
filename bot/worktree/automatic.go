package worktree

import (
	"bytes"
	"context"
	"strings"
	"time"
)

// AutomaticIsolationReason is empty only when HEAD represents the working
// copy. Never silently send a dirty-workspace review to an older checkout.
// Explicit worktree requests retain Open's HEAD-only semantics.
func AutomaticIsolationReason(ctx context.Context, cwd string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := output(ctx, cwd, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return "repository_unavailable"
	}
	prefix, err := output(ctx, cwd, "rev-parse", "--show-prefix")
	if err != nil {
		return "repository_unavailable"
	}
	if prefix != "" {
		kind, err := output(ctx, cwd, "cat-file", "-t", "HEAD:"+strings.TrimSuffix(prefix, "/"))
		if err != nil || kind != "tree" {
			return "working_directory_untracked"
		}
	}
	entries, err := git(ctx, cwd, nil, nil, "ls-files", "--stage", "-v", "-z", "--", ":/")
	if err != nil {
		return "repository_unavailable"
	}
	if reason := snapshotIndexReason(entries); reason != "" {
		return reason
	}
	status, err := git(ctx, cwd, nil, nil, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return "repository_unavailable"
	}
	if len(status) != 0 {
		return "uncommitted_changes"
	}
	return ""
}

// SnapshotIsolationReason checks whether the working copy can be copied with a
// private index. Unlike HEAD-only isolation it permits ordinary local changes.
func SnapshotIsolationReason(ctx context.Context, cwd string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, _, err := repository(ctx, cwd); err != nil {
		return "repository_unavailable"
	}
	if _, err := output(ctx, cwd, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return "repository_unavailable"
	}
	entries, err := git(ctx, cwd, nil, nil, "ls-files", "--stage", "-v", "-z", "--", ":/")
	if err != nil {
		return "repository_unavailable"
	}
	return snapshotIndexReason(entries)
}

func snapshotIndexReason(entries []byte) string {
	for _, entry := range bytes.Split(entries, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		if entry[0] == 'S' || entry[0] >= 'a' && entry[0] <= 'z' {
			return "index_flags"
		}
		header, _, _ := bytes.Cut(entry, []byte{'\t'})
		fields := bytes.Fields(header)
		if len(fields) != 4 || string(fields[3]) != "0" {
			return "index_conflict"
		}
		if string(fields[1]) == "160000" {
			return "submodules"
		}
	}
	return ""
}
