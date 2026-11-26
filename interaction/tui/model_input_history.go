package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	controlruntime "nekocode/runtime"
	"nekocode/util/filelock"
	"nekocode/util/fs"
)

const maxInputHistoryEntries = 200

type inputHistoryFile struct {
	Entries []string `json:"entries"`
}

func inputHistoryPath() string {
	return filepath.Join(fs.NekocodeHome(), "input_history.json")
}

func loadInputHistory() []string {
	entries, _ := readInputHistory(inputHistoryPath())
	return normalizeInputHistory(entries)
}

func readInputHistory(path string) ([]string, error) {
	f, err := fs.ReadJSONFile[inputHistoryFile](path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return f.Entries, err
}

func persistInputs(entries []string) ([]string, error) {
	path := inputHistoryPath()
	unlock, err := filelock.Acquire(path + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	history, err := readInputHistory(path)
	if err != nil {
		return nil, err
	}
	history = normalizeInputHistory(append(history, entries...))
	data, err := json.MarshalIndent(inputHistoryFile{Entries: history}, "", "  ")
	if err != nil {
		return nil, err
	}
	return history, fs.WritePrivateAtomic(path, data)
}

func appendInputHistory(entries []string, entry string) []string {
	return normalizeInputHistory(append(slices.Clip(entries), controlruntime.RedactInputText(entry)))
}

func normalizeInputHistory(entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == entry {
			continue
		}
		out = append(out, entry)
	}
	if len(out) > maxInputHistoryEntries {
		out = out[len(out)-maxInputHistoryEntries:]
	}
	return out
}

func (m *Model) rememberInput(entry string) {
	m.pendingInputHistory = appendInputHistory(m.pendingInputHistory, entry)
	history, err := persistInputs(m.pendingInputHistory)
	if err != nil {
		history = appendInputHistory(m.Input.History(), entry)
	} else {
		m.pendingInputHistory = nil
	}
	m.Input.SetHistory(history)
}
