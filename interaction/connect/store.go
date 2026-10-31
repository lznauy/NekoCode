// Package core provides the shared building blocks for IM connectors
// (telegram, feishu, QQ, ...): the connect.json file layer, the pairing
// state machine, the connector run-state base, shared text commands, the
// stream throttle, and the event-dispatch loop.
//
// The package offers composable parts, not a base class: each channel keeps
// its own package entry and wires these pieces together.
package connect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"nekocode/util/fs"
)

// fileStoreMu protects read/merge/write across independently created stores.
var fileStoreMu sync.Mutex

// FileStore reads and writes ~/.nekocode/connect.json one section at a
// time. Sections owned by other connectors are preserved verbatim as raw
// JSON, so concurrent connectors never clobber each other's configuration.
type FileStore struct {
	path string
}

// DefaultFileStore stores at ~/.nekocode/connect.json.
func DefaultFileStore() *FileStore {
	return &FileStore{path: filepath.Join(fs.NekocodeHome(), "connect.json")}
}

// Path returns the underlying file path.
func (s *FileStore) Path() string { return s.path }

// Load unmarshals one section into out. A missing file or section leaves
// out untouched (zero value).
func (s *FileStore) Load(section string, out any) error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	raw, ok := file[section]
	if !ok {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Save replaces one section, preserving other sections, and atomically
// replaces the file with 0600 permissions. Saves within this process serialize.
func (s *FileStore) Save(section string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fileStoreMu.Lock()
	defer fileStoreMu.Unlock()
	file := map[string]json.RawMessage{}
	data, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
	}
	if file == nil {
		file = make(map[string]json.RawMessage)
	}
	file[section] = raw
	data, err = json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".connect-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
