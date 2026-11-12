package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A repository-wide private ref keeps the baseline reachable by Git GC.
// Include both session and task identity; this never advances a user branch.
func (l *Lease) snapshotBaseRef() string {
	return "refs/nekocode/snapshots/" + filepath.Base(filepath.Dir(l.dir)) + "/" + filepath.Base(l.dir)
}

type workspaceSnapshot struct {
	base, tree string
	index      []byte
}

func (l *Lease) captureSnapshot(ctx context.Context, root, cwd, head string) (workspaceSnapshot, error) {
	fail := func(err error) (workspaceSnapshot, error) { return workspaceSnapshot{}, err }
	storage, err := canonical(l.dir)
	if err != nil {
		return fail(err)
	}
	if relative, err := filepath.Rel(root, storage); err != nil || relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fail(fmt.Errorf("snapshot storage is inside the source repository; use shared isolation"))
	}
	if reason := SnapshotIsolationReason(ctx, cwd); reason != "" {
		return fail(fmt.Errorf("cannot snapshot this workspace: %s", reason))
	}
	parentIndexPath, err := output(ctx, root, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return fail(err)
	}
	parentIndex, err := os.ReadFile(parentIndexPath)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	f, err := os.CreateTemp(l.dir, "snapshot-index-")
	if err != nil {
		return fail(err)
	}
	indexPath := f.Name()
	if len(parentIndex) > 0 {
		if _, err = f.Write(parentIndex); err != nil {
			f.Close()
			os.Remove(indexPath)
			return fail(err)
		}
	}
	if err = f.Close(); err != nil {
		os.Remove(indexPath)
		return fail(err)
	}
	defer os.Remove(indexPath)
	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if len(parentIndex) == 0 {
		if err = os.Remove(indexPath); err != nil {
			return fail(err)
		}
		if _, err = git(ctx, root, nil, env, "read-tree", "--empty"); err != nil {
			return fail(err)
		}
	}
	// Materialize split indexes while retaining intent-to-add and other index
	// flags. Rebuilding only ls-files entries would lose their staging meaning.
	if _, err = git(ctx, root, nil, env, "update-index", "--no-split-index"); err != nil {
		return fail(err)
	}
	stagedIndex, err := os.ReadFile(indexPath)
	if err != nil {
		return fail(err)
	}
	readTree := func() (string, error) {
		raw, err := git(ctx, root, nil, env, "write-tree")
		return string(bytes.TrimSpace(raw)), err
	}
	if _, err = git(ctx, root, nil, env, "add", "--all", "--", "."); err != nil {
		return fail(err)
	}
	// An untracked embedded repository can become a gitlink during add even
	// though the source index had no submodules. Never silently omit its files.
	entries, err := git(ctx, root, nil, env, "ls-files", "--stage", "-v", "-z", "--", ":/")
	if err != nil {
		return fail(err)
	}
	if reason := snapshotIndexReason(entries); reason != "" {
		return fail(fmt.Errorf("cannot snapshot this workspace: %s", reason))
	}
	tree, err := readTree()
	if err != nil {
		return fail(err)
	}
	// Detect changes while scanning, including newly staged/untracked files.
	// No source file or real index is ever rewritten during this operation.
	if _, err = git(ctx, root, nil, env, "add", "--all", "--", "."); err != nil {
		return fail(err)
	}
	checkTree, err := readTree()
	if err != nil {
		return fail(err)
	}
	checkIndex, err := os.ReadFile(parentIndexPath)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	checkHead, err := output(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fail(err)
	}
	if tree != checkTree || !bytes.Equal(parentIndex, checkIndex) || head != checkHead {
		return fail(fmt.Errorf("workspace changed while creating snapshot; retry after the writer settles"))
	}
	identity := []string{"GIT_AUTHOR_NAME=NekoCode", "GIT_AUTHOR_EMAIL=snapshot@nekocode.invalid", "GIT_COMMITTER_NAME=NekoCode", "GIT_COMMITTER_EMAIL=snapshot@nekocode.invalid"}
	raw, err := git(ctx, root, []byte("NekoCode private task snapshot\n"), identity, "commit-tree", tree, "-p", head)
	if err != nil {
		return fail(err)
	}
	return workspaceSnapshot{base: string(bytes.TrimSpace(raw)), tree: tree, index: stagedIndex}, nil
}

func (l *Lease) restoreSnapshot(ctx context.Context, captured workspaceSnapshot) error {
	// Populate the private files, then restore only its index to the parent's
	// staged tree. git status/diff retain the parent's staged/unstaged split.
	if _, err := git(ctx, l.Info.Path, nil, nil, "read-tree", "--reset", "-u", captured.tree); err != nil {
		return err
	}
	indexPath, err := output(ctx, l.Info.Path, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return err
	}
	if err = os.WriteFile(indexPath, captured.index, 0600); err != nil {
		return err
	}
	if err := os.MkdirAll(l.Info.Cwd, 0700); err != nil {
		return err
	}
	return nil
}
