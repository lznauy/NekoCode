package session

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rename changes only display metadata; IDs, timestamps and history stay intact.
// An empty name clears the custom label.
func (m *Manager) Rename(id, name string) error {
	if err := validateID(id); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("会话名称最多 80 个字符，且不能包含控制字符")
	}
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	unlock, err := lockSession(id)
	if err != nil {
		return err
	}
	defer unlock()
	sessions, err := os.OpenRoot(dir())
	if err != nil {
		return err
	}
	defer sessions.Close()
	info, err := sessions.Lstat(id)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("invalid session directory: %s", id)
	}
	root, err := sessions.OpenRoot(id)
	if err != nil {
		return err
	}
	defer root.Close()

	const manifest = "session.json"
	info, err = root.Lstat(manifest)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invalid session manifest: %s", id)
	}
	data, err := root.ReadFile(manifest)
	if err != nil {
		return err
	}
	var oldHeader snapshotHeader
	if err := json.Unmarshal(data, &oldHeader); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var storedID string
	if err := json.Unmarshal(fields["id"], &storedID); err != nil || storedID != id {
		return fmt.Errorf("invalid session manifest: %s", id)
	}
	if name == "" {
		delete(fields, "name")
	} else {
		fields["name"], _ = json.Marshal(name)
	}
	data, err = json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	if _, err := commitManifest(root, oldHeader, data); err != nil {
		return err
	}
	m.mu.Lock()
	if m.sess != nil && m.sess.ID == id {
		m.sess.Name = name
	}
	m.mu.Unlock()
	return nil
}
