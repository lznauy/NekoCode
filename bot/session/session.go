package session

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"nekocode/bot/provider/types"
	"nekocode/util/fs"
)

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	_, err = replaceRootFile(root, filepath.Base(path), data, perm)
	return err
}

// A directory sync can fail after the rename committed; retain that outcome
// so the owner can retry without treating its own manifest as another writer.
func replaceRootFile(root *os.Root, name string, data []byte, perm os.FileMode) (bool, error) {
	temp := ".session-" + rand.Text() + ".tmp"
	tmp, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return false, err
	}
	defer root.Remove(temp)
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := writeAndSync(tmp, data); err != nil {
		return false, err
	}
	if err := root.Rename(temp, name); err != nil {
		return false, err
	}
	folder, err := root.Open(".")
	if err != nil {
		return true, err
	}
	defer folder.Close()
	return true, syncDirectoryHandle(folder)
}

func writeAndSync(tmp *os.File, data []byte) error {
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	err = syncDirectoryHandle(dir)
	if closeErr := dir.Close(); err == nil {
		err = closeErr
	}
	return err
}

func syncDirectoryHandle(dir *os.File) error {
	// Windows FlushFileBuffers requires a writable handle and does not
	// support syncing an opened directory. File contents are synced before
	// publishing them by rename.
	if runtime.GOOS == "windows" {
		return nil
	}
	return dir.Sync()
}

// Manager owns only the current session identity and persistence lifecycle.
// Cross-module state capture and restore belongs to the bot assembly layer.
type Manager struct {
	mu        sync.Mutex
	persistMu sync.Mutex
	cwd       string
	sess      *Snapshot
}

// DefaultExportPath is the default context-export destination under ~/.nekocode/exports.
// Applications can override it before exporting. An unavailable home leaves
// it empty until an export can resolve a valid home.
var DefaultExportPath, _ = userExportPath()

func userExportPath() (string, error) {
	home, err := fs.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve session export directory: %w", err)
	}
	return filepath.Join(home, ".nekocode", "exports", "nekocode-context.json"), nil
}

func resolveExportPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	if DefaultExportPath != "" {
		return DefaultExportPath, nil
	}
	return userExportPath()
}

// New creates a manager with a fresh current session.
func New(cwd string) *Manager {
	return &Manager{cwd: cwd, sess: newSnapshot(cwd)}
}

// Current returns a detached snapshot. Changes become visible only through
// Save or Activate, never by mutating state after the manager lock is released.
func (m *Manager) Current() *Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sess.Clone()
}

func (m *Manager) CurrentID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil {
		return ""
	}
	return m.sess.ID
}

func (m *Manager) set(sess *Snapshot) {
	m.mu.Lock()
	m.sess = sess.Clone()
	m.mu.Unlock()
}

// StartNew replaces the current session identity.
func (m *Manager) StartNew() *Snapshot {
	sess := newSnapshot(m.cwd)
	m.set(sess)
	return sess
}

// ClearCurrent drops the current session identity.
func (m *Manager) ClearCurrent() {
	m.set(nil)
}

// IsDraft reports whether this session needs in-memory restoration because it
// has neither persisted conversation history nor a session goal.
func (s *Snapshot) IsDraft() bool {
	return s.Goal == nil && len(s.Transcript) == 0 && len(s.Messages) == 0 && s.TranscriptSeq == 0 && s.TranscriptBase == 0
}

// Save persists the current session or removes its disposable draft.
func (m *Manager) Save(sess *Snapshot) error {
	return m.save(sess, false)
}

// SavePreservingDraft keeps an empty conversation's active identity in memory
// without creating a saved-history entry. UI preferences and pending attachments
// belong to that draft until the user explicitly starts another session.
func (m *Manager) SavePreservingDraft(sess *Snapshot) error {
	return m.save(sess, true)
}

func (m *Manager) save(sess *Snapshot, keepDraft bool) error {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	if sess == nil {
		return fmt.Errorf("session: cannot save nil snapshot")
	}
	if sess.IsDraft() {
		if err := sess.discardDraft(); err != nil {
			return err
		}
		if keepDraft {
			m.set(sess)
		} else {
			m.clearCurrentIfID(sess.ID)
		}
		return nil
	}
	base := sess.revision
	err := sess.save()
	if err != nil && base == sess.revision {
		return err
	}
	if sess.TranscriptLazy {
		saved := *sess
		saved.Transcript = nil
		saved.TranscriptBase = saved.TranscriptSeq
		sess = &saved
	}
	m.set(sess)
	return err
}

func (s *Snapshot) discardDraft() error {
	unlock, err := lockSession(s.ID)
	if err != nil {
		return err
	}
	defer unlock()
	d := filepath.Join(dir(), s.ID)
	if _, _, err := s.readBase(filepath.Join(d, "session.json")); err != nil {
		return err
	}
	// A goal-only session may have a journal append awaiting its first
	// manifest commit. A detached empty draft cannot discard that turn.
	info, err := os.Stat(filepath.Join(d, transcriptFileName))
	if err == nil && info.Size() > 0 {
		return fmt.Errorf("%w: session has pending history", ErrSessionConflict)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(d); err != nil {
		return err
	}
	s.revision = nil
	return nil
}

func (m *Manager) clearCurrentIfID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess != nil && m.sess.ID == id {
		m.sess = nil
	}
}

// Load validates and reads a persisted session without changing the current
// session identity. Callers can finish fail-closed cleanup before activation.
func (m *Manager) Load(id string) (*Snapshot, error) {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	sess, err := load(id)
	if err != nil {
		return nil, fmt.Errorf("session: load: %w", err)
	}
	return sess, nil
}

// LoadLazy restores active context and history metadata without retaining old
// transcript bodies. Load remains an explicit full-history compatibility API.
func (m *Manager) LoadLazy(id string) (*Snapshot, error) {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	return loadMode(id, true)
}

// Activate replaces the current session with a previously loaded snapshot.
func (m *Manager) Activate(sess *Snapshot) error {
	if sess == nil || sess.ID == "" {
		return fmt.Errorf("session: cannot activate an empty snapshot")
	}
	m.set(sess)
	return nil
}

func (m *Manager) List() []Meta {
	return list()
}

// Exists checks the durable session record without parsing it. This is used
// by crash recovery, where an unreadable record must not be mistaken for a
// successfully deleted session.
func (m *Manager) Exists(id string) (bool, error) {
	if err := validateID(id); err != nil {
		return false, err
	}
	_, err := os.Stat(filepath.Join(dir(), id, "session.json"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (m *Manager) Delete(id string) error {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	return deleteSnapshot(id)
}

func ExportMessages(msgs []types.Message, path string) (string, error) {
	path, err := resolveExportPath(path)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal context: %w", err)
	}
	if err := fs.WriteFileWithDir(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return path, nil
}

// CurrentGoal returns an independent copy without cloning conversation history.
func (m *Manager) CurrentGoal() *Goal {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil || m.sess.Goal == nil {
		return nil
	}
	return m.sess.Goal.clone()
}
