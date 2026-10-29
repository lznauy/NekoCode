package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (m *Manager) StatusReport() (string, error) {
	if m.indexer == nil {
		return "Project index unavailable: no supported project marker found in this workspace.", nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.indexer.Status()
	if err != nil {
		return "", err
	}
	langs := make([]string, 0, len(s.Languages))
	for lang, n := range s.Languages {
		langs = append(langs, fmt.Sprintf("%s: %d", lang, n))
	}
	sort.Strings(langs)
	last := s.LastSync
	if last == "" {
		last = "unknown"
	}
	watcher := "unavailable (rebuild to retry)"
	if m.syncer != nil {
		state, watchErr := m.syncer.Status()
		watcher = state
		if watchErr != nil {
			watcher += fmt.Sprintf(" (last error: %v)", watchErr)
		}
	}
	return fmt.Sprintf("Project index\nRoot: %s\nFiles: %d\nLanguages: %s\nLast successful sync (UTC): %s\nWatcher: %s", m.root, s.Files, strings.Join(langs, ", "), last, watcher), nil
}

// BackupDatabase preserves the closed database and its SQLite sidecars before
// rebuilding. If rollback leaves the active database incomplete, the returned
// Manager owns the exclusive lease; the caller must Close it after recovery.
func BackupDatabase(cwd string) (manager *Manager, backup string, err error) {
	root := findProjectRoot(cwd)
	if root == "" {
		return nil, "", fmt.Errorf("no supported project marker found in this workspace")
	}

	lease, err := acquireIndexLease(root, true)
	if err != nil {
		return nil, "", err
	}
	manager = &Manager{cwd: cwd, root: root, lease: lease}
	backup, err = backupDatabase(root)
	if err != nil && recoveryIncomplete(err) {
		return manager, backup, err
	}
	if closeErr := manager.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("release backup lease: %w", closeErr))
	}
	manager = nil
	return manager, backup, err
}

// RebuildManager keeps other processes from opening the database between the
// backup and replacement initialization. The caller must close its old Manager.
func RebuildManager(cwd string) (manager *Manager, backup string, err error) {
	root := findProjectRoot(cwd)
	if root == "" {
		return nil, "", fmt.Errorf("no supported project marker found in this workspace")
	}
	lease, err := acquireIndexLease(root, true)
	if err != nil {
		return nil, "", err
	}
	manager = &Manager{cwd: cwd, root: root, lease: lease}
	backup, err = backupDatabase(root)
	if err != nil {
		if !recoveryIncomplete(err) {
			if closeErr := manager.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("release rebuild lease: %w", closeErr))
			}
			manager = nil
		}
		return manager, backup, err
	}
	defer func() {
		if err == nil {
			return
		}
		var releaseLease bool
		releaseLease, err = rollbackReplacement(root, backup, manager, err)
		if !releaseLease {
			if manager == nil {
				manager = &Manager{cwd: cwd, root: root, lease: lease}
			}
			return
		}
		var closeErr error
		if manager != nil {
			closeErr = manager.releaseLease()
		} else {
			closeErr = lease.close()
		}
		manager = nil
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("release rebuild lease: %w", closeErr))
		}
	}()
	opened, openErr := newManagerWithLease(cwd, root, lease)
	if opened != nil {
		manager = opened
	}
	err = openErr
	if err == nil {
		err = initializeManager(manager)
	}
	if err == nil {
		err = lease.share()
	}
	return manager, backup, err
}

var initializeManager = func(manager *Manager) error {
	return manager.Init()
}

func rollbackReplacement(root, backup string, manager *Manager, retErr error) (bool, error) {
	if manager != nil {
		// Keep the exclusive lease in RebuildManager until recovery is complete.
		if err := manager.close(false); err != nil {
			return false, errors.Join(retErr, fmt.Errorf("close failed replacement: %w", err))
		}
	}
	quarantine, err := backupDatabase(root)
	if err != nil {
		return false, errors.Join(retErr, fmt.Errorf("quarantine failed replacement (quarantine: %s): %w", quarantine, err))
	}
	if err := restoreDatabase(root, backup); err != nil {
		return false, errors.Join(retErr, fmt.Errorf("restore database backup: %w", err))
	}
	return true, retErr
}

func restoreDatabase(root, backup string) (retErr error) {
	if backup == "" {
		return nil
	}
	path := filepath.Join(root, ".nekocode", "index.db")
	var moved []string
	defer func() {
		if retErr == nil {
			return
		}
		for i := len(moved) - 1; i >= 0; i-- {
			suffix := moved[i]
			if err := renameIndexDBFile(path+suffix, filepath.Join(backup, "index.db"+suffix)); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("return restored index database%s to backup: %w", suffix, err))
			}
		}
	}()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		source := filepath.Join(backup, "index.db"+suffix)
		if _, err := os.Lstat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := renameIndexDBFile(source, path+suffix); err != nil {
			return fmt.Errorf("restore index database%s: %w", suffix, err)
		}
		moved = append(moved, suffix)
	}
	return nil
}

var renameIndexDBFile = os.Rename

type incompleteRecoveryError struct {
	err error
}

func (e *incompleteRecoveryError) Error() string { return e.err.Error() }
func (e *incompleteRecoveryError) Unwrap() error { return e.err }

func recoveryIncomplete(err error) bool {
	var recoveryErr *incompleteRecoveryError
	return errors.As(err, &recoveryErr)
}

func backupDatabase(root string) (backup string, retErr error) {
	path := filepath.Join(root, ".nekocode", "index.db")
	var moved []string
	defer func() {
		if retErr == nil || backup == "" {
			return
		}
		recoveryFailed := false
		for i := len(moved) - 1; i >= 0; i-- {
			suffix := moved[i]
			if err := renameIndexDBFile(filepath.Join(backup, "index.db"+suffix), path+suffix); err != nil {
				recoveryFailed = true
				retErr = errors.Join(retErr, fmt.Errorf("restore index database%s: %w", suffix, err))
			}
		}
		if recoveryFailed {
			retErr = &incompleteRecoveryError{err: retErr}
		}
	}()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return backup, err
		}
		if backup == "" {
			var err error
			backup, err = os.MkdirTemp(filepath.Dir(path), "index-backup-")
			if err != nil {
				return "", err
			}
		}
		if err := renameIndexDBFile(path+suffix, filepath.Join(backup, "index.db"+suffix)); err != nil {
			return backup, err
		}
		moved = append(moved, suffix)
	}
	return backup, nil
}
