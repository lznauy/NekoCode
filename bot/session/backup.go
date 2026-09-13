package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BackupCurrent copies the durable pre-compaction snapshot without replacing
// any prior backup. The transcript is append-only and shared by every backup.
func (m *Manager) BackupCurrent(compactionID string) (string, error) {
	id := m.CurrentID()
	if err := validateID(id); err != nil {
		return "", err
	}
	if err := validateID(compactionID); err != nil {
		return "", fmt.Errorf("invalid compaction id: %w", err)
	}
	source := filepath.Join(dir(), id, "session.json")
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	backupDir := filepath.Join(dir(), id, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("pre-compact-%s-%s.session.json", time.Now().UTC().Format("20060102T150405.000000000Z"), compactionID)
	path := filepath.Join(backupDir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	backupData, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(backupData, data) {
		_ = os.Remove(path)
		if err == nil {
			err = fmt.Errorf("backup verification mismatch")
		}
		return "", err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(backupData, &snapshot); err != nil || snapshot.ID != id {
		_ = os.Remove(path)
		if err == nil {
			err = fmt.Errorf("backup session id mismatch")
		}
		return "", err
	}
	if err := syncDir(backupDir); err != nil {
		return "", err
	}
	return path, nil
}
