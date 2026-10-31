package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Only a disposable read model: session.json remains authoritative. Checking
// its nanosecond mtime and size also detects changes made by another process.
type listProjection struct {
	Version    int   `json:"version"`
	SourceSize int64 `json:"source_size"`
	SourceTime int64 `json:"source_time"`
	Meta       Meta  `json:"meta"`
}

func loadMeta(id string) (Meta, error) { return loadMetaAt(dir(), id) }

func loadMetaAt(root, id string) (Meta, error) {
	path := filepath.Join(root, id, "session.json")
	info, err := os.Stat(path)
	if err != nil {
		return Meta{}, err
	}
	// Preserve the original list reader's permission repair without issuing
	// a chmod syscall for every correctly protected session on every listing.
	if info.Mode().Perm() != 0600 {
		_ = os.Chmod(path, 0600)
	}
	cachePath := filepath.Join(root, id, "list-meta.json")
	var cached listProjection
	if data, err := os.ReadFile(cachePath); err == nil && json.Unmarshal(data, &cached) == nil && cached.Version == 1 && cached.SourceSize == info.Size() && cached.SourceTime == info.ModTime().UnixNano() && cached.Meta.ID == id {
		return cached.Meta, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, err
	}
	meta, err := decodeMeta(data)
	if err != nil {
		return Meta{}, err
	}
	if meta.ID == id {
		projection := listProjection{Version: 1, SourceSize: info.Size(), SourceTime: info.ModTime().UnixNano(), Meta: meta}
		if data, err := json.Marshal(projection); err == nil {
			// Read-only archives and full disks still allow listing via the manifest.
			_ = writeListProjection(cachePath, data)
		}
	}
	return meta, nil
}

func writeListProjection(path string, data []byte) error {
	// Cache writes may race deletion. Never create the parent directory here:
	// a list reader must not recreate a deleted session or its fork source.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".list-meta-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// This cache is disposable, so durability fsyncs would only add latency.
	// Rename publishes complete JSON; incomplete cache data is never exposed.
	return os.Rename(tmp.Name(), path)
}
