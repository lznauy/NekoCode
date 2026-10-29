// Package worktree manages durable, opt-in Git working copies for delegated tasks.
package worktree

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var validBase = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

type Info struct {
	TaskID     string `json:"task_id"`
	Repository string `json:"repository"`
	CommonDir  string `json:"common_dir"`
	Base       string `json:"base"`
	Path       string `json:"path"`
	Cwd        string `json:"cwd"`
	Removed    bool   `json:"removed,omitempty"`
}

type Lease struct {
	dir  string
	lock *os.File
	Info Info
}

// Acquire also works for orphaned creations with no saved task transcript.
// Lock files are permanent so cleanup cannot create a second lock identity.
func Acquire(parent, id string) (*Lease, error) {
	if !validID.MatchString(parent) || !validID.MatchString(id) {
		return nil, fmt.Errorf("invalid session or task ID")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".nekocode", "worktrees", parent, id)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "active.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	ok, err := tryLock(f)
	if err != nil || !ok {
		f.Close()
		if err == nil {
			err = fmt.Errorf("worktree task %s is active", id)
		}
		return nil, err
	}
	return &Lease{dir: dir, lock: f}, nil
}
func (l *Lease) Close() error {
	if l.lock == nil {
		return nil
	}
	f := l.lock
	l.lock = nil
	return f.Close()
}
func canonical(path string) (string, error) {
	p, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	return filepath.EvalSymlinks(p)
}
func output(ctx context.Context, cwd string, args ...string) (string, error) {
	b, e := git(ctx, cwd, nil, nil, args...)
	return strings.TrimSpace(string(b)), e
}
func repository(ctx context.Context, cwd string) (root, common string, err error) {
	root, err = output(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return
	}
	root, err = canonical(root)
	if err != nil {
		return
	}
	common, err = output(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err == nil {
		common, err = canonical(common)
	}
	return
}
func (l *Lease) save() error {
	b, e := json.MarshalIndent(l.Info, "", "  ")
	if e != nil {
		return e
	}
	p := filepath.Join(l.dir, "meta.json")
	tmp := p + ".tmp"
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, p)
}

// Open creates from HEAD or validates an existing worktree. Never silently
// replaces a missing worktree during task resume.
func (l *Lease) Open(ctx context.Context, cwd, id string, resume bool) (Info, error) {
	root, common, err := repository(ctx, cwd)
	if err != nil {
		return Info{}, err
	}
	data, err := os.ReadFile(filepath.Join(l.dir, "meta.json"))
	if err == nil {
		if !resume {
			return Info{}, fmt.Errorf("worktree already exists; inspect /worktree list")
		}
		if err = json.Unmarshal(data, &l.Info); err != nil {
			return Info{}, err
		}
		if err = l.validate(ctx, root, common, id); err != nil {
			return Info{}, err
		}
		return l.Info, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Info{}, err
	}
	if resume {
		return Info{}, fmt.Errorf("saved worktree is missing; refusing to recreate it")
	}
	head, err := output(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Info{}, err
	}
	realcwd, err := canonical(cwd)
	if err != nil {
		return Info{}, err
	}
	rel, err := filepath.Rel(root, realcwd)
	if err != nil {
		return Info{}, err
	}
	path := filepath.Join(l.dir, "tree")
	l.Info = Info{TaskID: id, Repository: root, CommonDir: common, Base: head, Path: path, Cwd: filepath.Join(path, rel)}
	// Persist before creation so a crash still leaves an observable recovery record.
	if err = l.save(); err != nil {
		return Info{}, err
	}
	if _, err = git(ctx, root, nil, nil, "worktree", "add", "--detach", "--no-checkout", "--", path, head); err != nil {
		return Info{}, err
	}
	// Conditional includes can activate only inside the new worktree. Discover
	// and disable its filters before the first checkout of repository files.
	if _, err = git(ctx, path, nil, nil, "reset", "--hard", head); err != nil {
		return Info{}, err
	}
	if err = l.validate(ctx, root, common, id); err != nil {
		return Info{}, err
	}
	return l.Info, nil
}
func (l *Lease) validate(ctx context.Context, root, common, id string) error {
	i := l.Info
	if i.Removed {
		return fmt.Errorf("worktree was removed; start a new task")
	}
	if !validBase.MatchString(i.Base) {
		return fmt.Errorf("invalid worktree base commit")
	}
	if i.TaskID != id || i.Repository != root || i.CommonDir != common || i.Path != filepath.Join(l.dir, "tree") {
		return fmt.Errorf("worktree identity mismatch")
	}
	path, err := canonical(i.Path)
	if err != nil || path != i.Path {
		return fmt.Errorf("worktree path missing or replaced")
	}
	rel, err := filepath.Rel(i.Path, i.Cwd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid worktree cwd")
	}
	realcwd, err := canonical(i.Cwd)
	if err != nil || realcwd != i.Cwd {
		return fmt.Errorf("worktree cwd missing or replaced")
	}
	actual, actualCommon, err := repository(ctx, i.Path)
	if err != nil {
		return err
	}
	if actual != i.Path || actualCommon != common {
		return fmt.Errorf("worktree repository mismatch")
	}
	return nil
}

func (l *Lease) Load(ctx context.Context, cwd, id string) (Info, error) {
	return l.Open(ctx, cwd, id, true)
}

// ApplyCheckpoint is called while the repository apply lock is held. Begin
// runs only after a non-empty patch passes the apply preflight. Every Capture
// is completed before any working-copy write, and each successful Capture has
// a matching Finalize even when the real apply fails.
type ApplyCheckpoint interface {
	Begin() error
	Capture(path string) error
	Finalize(path string) error
}

// Diff uses a temporary index: new files are included without changing the
// task's staging state. The saved base includes any commits made in the task.
func (l *Lease) Diff(ctx context.Context) ([]byte, error) {
	patch, _, err := l.diff(ctx)
	return patch, err
}

// diff builds both representations from one temporary index. The raw plan is
// deliberately independent of patch text parsing, including for binary files
// and paths containing whitespace or newlines.
func (l *Lease) diff(ctx context.Context) (patch, raw []byte, err error) {
	f, err := os.CreateTemp(l.dir, "diff-index-")
	if err != nil {
		return nil, nil, err
	}
	p := f.Name()
	f.Close()
	os.Remove(p)
	defer os.Remove(p)
	// Preserve the actual tracked set, including force-added ignored files and
	// entries staged since HEAD. Reconstruct instead of modifying the real index.
	entries, err := git(ctx, l.Info.Path, nil, nil, "ls-files", "--stage", "--full-name", "-z")
	if err != nil {
		return nil, nil, err
	}
	env := []string{"GIT_INDEX_FILE=" + p}
	if _, err = git(ctx, l.Info.Path, entries, env, "update-index", "-z", "--index-info"); err != nil {
		return nil, nil, err
	}
	if _, err = git(ctx, l.Info.Path, nil, env, "add", "--all", "--", "."); err != nil {
		return nil, nil, err
	}
	base := []string{"diff", "--cached", "--no-color", "--no-relative", "--no-ext-diff", "--no-textconv"}
	patch, err = git(ctx, l.Info.Path, nil, env, append(append([]string{}, base...), "--binary", "--src-prefix=a/", "--dst-prefix=b/", l.Info.Base, "--")...)
	if err != nil {
		return nil, nil, err
	}
	raw, err = git(ctx, l.Info.Path, nil, env, append(append([]string{}, base...), "--raw", "-z", "--no-renames", l.Info.Base, "--")...)
	if err != nil {
		return nil, nil, err
	}
	return patch, raw, nil
}

func checkpointPaths(root string, raw []byte) ([]string, error) {
	fields := bytes.Split(raw, []byte{0})
	paths := make([]string, 0, len(fields)/2)
	for i := 0; i < len(fields); i++ {
		if len(fields[i]) == 0 {
			continue
		}
		meta := strings.Fields(string(fields[i]))
		if len(meta) != 5 || !strings.HasPrefix(meta[0], ":") || i+1 >= len(fields) {
			return nil, fmt.Errorf("invalid raw worktree diff record")
		}
		i++
		name := string(fields[i])
		for _, mode := range []string{strings.TrimPrefix(meta[0], ":"), meta[1]} {
			if mode == "000000" {
				continue
			}
			value, err := strconv.ParseUint(mode, 8, 32)
			if err != nil || value&0170000 != 0100000 {
				return nil, fmt.Errorf("checkpoint does not support worktree path %q with mode %s", name, mode)
			}
		}
		rel := filepath.FromSlash(name)
		if name == "" || filepath.IsAbs(rel) || rel == "." {
			return nil, fmt.Errorf("invalid worktree diff path %q", name)
		}
		path := filepath.Join(root, rel)
		back, err := filepath.Rel(root, path)
		if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("worktree diff path escapes repository: %q", name)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (l *Lease) Apply(ctx context.Context, checkpoints ...ApplyCheckpoint) (changed bool, err error) {
	// Git apply does not lock working-copy files. Serialize our integrations
	// across tasks, sessions and processes before checking or rewriting them.
	lock, err := os.OpenFile(filepath.Join(l.Info.CommonDir, "nekocode-worktree-apply.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	for {
		ok, err := tryLock(lock)
		if err != nil {
			return false, err
		}
		if ok {
			break
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}

	patch, raw, err := l.diff(ctx)
	if err != nil {
		return false, err
	}
	if len(patch) == 0 {
		return false, nil
	}
	var cp ApplyCheckpoint
	if len(checkpoints) > 0 {
		cp = checkpoints[0]
	}
	var paths []string
	if cp != nil {
		paths, err = checkpointPaths(l.Info.Repository, raw)
		if err != nil {
			return false, err
		}
	}
	if _, err = git(ctx, l.Info.Repository, patch, nil, "apply", "--check", "--whitespace=nowarn", "--binary", "-"); err != nil {
		return false, err
	}
	captured := make([]string, 0, len(paths))
	if cp != nil {
		if err = cp.Begin(); err != nil {
			return false, err
		}
		defer func() {
			for _, path := range captured {
				err = errors.Join(err, cp.Finalize(path))
			}
		}()
		for _, path := range paths {
			if err = cp.Capture(path); err != nil {
				return false, err
			}
			captured = append(captured, path)
		}
	}
	if _, err = git(ctx, l.Info.Repository, patch, nil, "apply", "--whitespace=nowarn", "--binary", "-"); err != nil {
		return false, err
	}
	return true, nil
}

// LoadForRemoval tolerates incomplete creation, but never trusts saved paths
// outside this task directory. Only explicit discard uses this recovery path.
func (l *Lease) LoadForRemoval(ctx context.Context, cwd, id string) (Info, error) {
	root, common, err := repository(ctx, cwd)
	if err != nil {
		return Info{}, err
	}
	b, err := os.ReadFile(filepath.Join(l.dir, "meta.json"))
	if err != nil {
		return Info{}, err
	}
	if err = json.Unmarshal(b, &l.Info); err != nil {
		return Info{}, err
	}
	i := l.Info
	if i.TaskID != id || i.Repository != root || i.CommonDir != common || i.Path != filepath.Join(l.dir, "tree") {
		return Info{}, fmt.Errorf("worktree identity mismatch")
	}
	if i.Removed {
		return Info{}, fmt.Errorf("worktree already removed")
	}
	actualDir, err := canonical(l.dir)
	if err != nil || actualDir != l.dir {
		return Info{}, fmt.Errorf("worktree management directory replaced")
	}
	if stat, err := os.Lstat(i.Path); err == nil && stat.Mode()&os.ModeSymlink != 0 {
		return Info{}, fmt.Errorf("worktree path replaced by symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Info{}, err
	}
	return i, nil
}

func (l *Lease) Remove(ctx context.Context, discard bool) error {
	if !discard {
		status, err := git(ctx, l.Info.Path, nil, nil, "status", "--porcelain=v1", "--ignored=matching", "--untracked-files=all", "-z")
		if err != nil {
			return err
		}
		if len(status) > 0 {
			return fmt.Errorf("worktree contains modified, untracked or ignored files; review before remove <id> --discard")
		}
		patch, err := l.Diff(ctx)
		if err != nil {
			return err
		}
		if len(patch) > 0 {
			return fmt.Errorf("worktree contains changes; use remove <id> --discard only after reviewing or applying them")
		}
	}
	listed, err := git(ctx, l.Info.Repository, nil, nil, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	registered := false
	for _, record := range bytes.Split(listed, []byte{0}) {
		if string(record) == "worktree "+l.Info.Path {
			registered = true
		}
	}
	if registered {
		if _, err := git(ctx, l.Info.Repository, nil, nil, "worktree", "remove", "--force", "--", l.Info.Path); err != nil {
			return err
		}
	} else if !discard {
		return fmt.Errorf("worktree registration missing; review before explicit discard")
	}
	// Interrupted creation may leave files without a registered worktree.
	dir, err := os.OpenRoot(l.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = dir.RemoveAll("tree"); err != nil {
		return err
	}
	l.Info.Removed = true
	return l.save()
}

func List(parent string) ([]Info, error) {
	if !validID.MatchString(parent) {
		return nil, fmt.Errorf("invalid session ID")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".nekocode", "worktrees", parent)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []Info
	for _, entry := range entries {
		if !entry.IsDir() || !validID.MatchString(entry.Name()) {
			continue
		}
		b, e := os.ReadFile(filepath.Join(dir, entry.Name(), "meta.json"))
		var i Info
		if e == nil && json.Unmarshal(b, &i) == nil && !i.Removed && i.TaskID == entry.Name() {
			result = append(result, i)
		}
	}
	return result, nil
}
