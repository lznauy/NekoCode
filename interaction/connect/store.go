// Package connect provides the shared building blocks for IM connectors
// (telegram, feishu, QQ, ...): the connect.json file layer, the pairing
// state machine, the connector run-state base, shared text commands, the
// stream throttle, and the event-dispatch loop.
//
// The package offers composable parts, not a base class: each channel keeps
// its own package entry and wires these pieces together.
package connect

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"nekocode/util/filelock"
	"nekocode/util/fs"
)

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
	_, err := s.load(section, out)
	return err
}

func (s *FileStore) readSections() (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sections map[string]json.RawMessage
	err = json.Unmarshal(data, &sections)
	return sections, err
}

func (s *FileStore) load(section string, out any) (json.RawMessage, error) {
	sections, err := s.readSections()
	if err != nil {
		return nil, err
	}
	raw := sections[section]
	if raw == nil {
		return nil, nil
	}
	return raw, json.Unmarshal(raw, out)
}

// SectionVersion identifies the observed section, including edits made outside NekoCode.
type SectionVersion struct{ digest [sha256.Size]byte }

var ErrConfigChanged = errors.New("configuration changed; retry the command")

func sectionVersion(raw json.RawMessage) (SectionVersion, error) {
	if raw == nil {
		return SectionVersion{}, nil
	}
	// Match encoding/json's formatting of untouched sections on the next write.
	compact, err := json.Marshal(raw)
	return SectionVersion{digest: sha256.Sum256(compact)}, err
}

// LoadVersioned reads the config and its comparison token from the same file snapshot.
func (s *FileStore) LoadVersioned(section string, out any) (*SectionVersion, error) {
	raw, err := s.load(section, out)
	if err != nil {
		return nil, err
	}
	version, err := sectionVersion(raw)
	return &version, err
}

// SaveVersioned replaces an object config if expected still matches. A nil expected
// requests an unconditional replacement. Even a reset to identical settings
// invalidates prior snapshots through the revision stored inside this section.
func (s *FileStore) SaveVersioned(section string, v any, expected *SectionVersion) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("connector config must be an object")
	}
	fields["_revision"], err = json.Marshal(rand.Text())
	if err != nil {
		return err
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	return s.replace(section, raw, expected)
}

// Save replaces one section, preserving other sections, and atomically
// replaces the file with 0600 permissions.
func (s *FileStore) Save(section string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.replace(section, raw, nil)
}

func (s *FileStore) replace(section string, raw json.RawMessage, expected *SectionVersion) error {
	unlock, err := filelock.Acquire(s.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	file, err := s.readSections()
	if err != nil {
		return err
	}
	if expected != nil {
		current, err := sectionVersion(file[section])
		if err != nil {
			return err
		}
		if current != *expected {
			return ErrConfigChanged
		}
	}
	if file == nil {
		file = make(map[string]json.RawMessage)
	}
	file[section] = raw
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return fs.WritePrivateAtomic(s.path, append(data, '\n'))
}
