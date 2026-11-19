package runner

import (
	"context"
	"crypto/sha256"
	"io"
	"io/fs"
	"path/filepath"

	"nekocode/bot/extension/tool/runtime/permission"
	utilfs "nekocode/util/fs"
)

// PreservedFileDigest is a non-interactive read of a file named by the user.
// The caller supplies the original workspace so a later /cd cannot redirect it.
func (e *Executor) PreservedFileDigest(ctx context.Context, root, path string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	policy := e.permissionSnapshot()
	if err := ctx.Err(); err != nil {
		return digest, err
	}
	path = filepath.FromSlash(path)
	if root == "" || e.workspace.Primary() != root || !filepath.IsLocal(path) || filepath.Clean(path) != path {
		return digest, fs.ErrPermission
	}
	if e.registry == nil || !e.registry.Has("read") || e.CheckToolPolicy("read") != nil {
		return digest, fs.ErrPermission
	}
	full, _, allowed, err := e.workspace.CheckReadLexical(filepath.Join(root, path))
	if err != nil || !allowed || policy.engine == nil {
		return digest, fs.ErrPermission
	}
	info := permission.BuildCallInfo("read", map[string]any{"path": full}, policy.workspace, policy.home)
	if policy.engine.EvaluateContext(ctx, "read", info, defaultPermissionEffect("read")).Effect != permission.EffectAllow {
		return digest, fs.ErrPermission
	}
	file, err := utilfs.OpenRegularNoFollow(full)
	if err != nil {
		return digest, err
	}
	defer file.Close()
	const limit = 16 << 20
	stat, err := file.Stat()
	if err != nil {
		return digest, err
	}
	if stat.Size() > limit {
		return digest, utilfs.ErrFileTooLarge
	}
	if !policy.current(e) || e.CheckToolPolicy("read") != nil {
		return digest, fs.ErrPermission
	}
	stop := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer stop()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, limit+1))
	if err != nil {
		return digest, err
	}
	if size > limit {
		return digest, utilfs.ErrFileTooLarge
	}
	if ctx.Err() != nil || !policy.current(e) || e.CheckToolPolicy("read") != nil {
		return digest, fs.ErrPermission
	}
	hash.Sum(digest[:0])
	return digest, nil
}
