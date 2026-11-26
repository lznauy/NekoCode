package tui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"nekocode/interaction/tui/components"
)

func TestNormalizeInputHistory(t *testing.T) {
	var entries []string
	for i := 0; i < maxInputHistoryEntries+5; i++ {
		entries = append(entries, fmt.Sprintf("cmd-%03d", i))
	}
	entries = append([]string{"", "  first  ", "first"}, entries...)

	got := normalizeInputHistory(entries)
	if len(got) != maxInputHistoryEntries {
		t.Fatalf("len = %d, want %d", len(got), maxInputHistoryEntries)
	}
	if got[0] != "cmd-005" {
		t.Fatalf("oldest retained entry = %q, want cmd-005", got[0])
	}
	if got[len(got)-1] != "cmd-204" {
		t.Fatalf("last entry = %q, want cmd-204", got[len(got)-1])
	}
}

func isolateInputHistory(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestInputHistoryPersistsAndReloads(t *testing.T) {
	isolateInputHistory(t)

	if _, err := persistInputs([]string{"first command", "second command"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(inputHistoryPath()); err != nil {
		t.Fatalf("history file was not written: %v", err)
	}

	in := components.NewInput(80)
	in.SetHistory(loadInputHistory())
	in.HistoryUp()
	if got := in.Value(); got != "second command" {
		t.Fatalf("first history up = %q, want second command", got)
	}
	in.HistoryUp()
	if got := in.Value(); got != "first command" {
		t.Fatalf("second history up = %q, want first command", got)
	}
}

func TestTwoOpenComposersPreserveBothInputHistories(t *testing.T) {
	isolateInputHistory(t)
	first := &Model{Input: components.NewInput(80)}
	second := &Model{Input: components.NewInput(80)}
	first.rememberInput("from the first window")
	second.rememberInput("from the second window")
	first.rememberInput("final message")
	want := []string{"from the first window", "from the second window", "final message"}
	if got := loadInputHistory(); !slices.Equal(got, want) {
		t.Fatalf("saved history = %q, want %q", got, want)
	}
}

func TestInputHistoryRedactsTelegramToken(t *testing.T) {
	history := appendInputHistory(nil, "/connect telegram token 123:super-secret")
	if len(history) != 1 {
		t.Fatalf("history len = %d", len(history))
	}
	if strings.Contains(history[0], "super-secret") || !strings.Contains(history[0], "[redacted]") {
		t.Fatalf("history was not redacted: %q", history[0])
	}

	history = appendInputHistory(nil, "/connect telegram add personal 123:super-secret")
	if len(history) != 1 {
		t.Fatalf("history len = %d", len(history))
	}
	if strings.Contains(history[0], "super-secret") || history[0] != "/connect telegram add personal [redacted]" {
		t.Fatalf("add history was not redacted: %q", history[0])
	}

	history = appendInputHistory(nil, "/connect telegram add 123:super-secret")
	if len(history) != 1 {
		t.Fatalf("history len = %d", len(history))
	}
	if strings.Contains(history[0], "super-secret") || history[0] != "/connect telegram add [redacted]" {
		t.Fatalf("short add history was not redacted: %q", history[0])
	}
}

func TestInputHistoryRetriesPendingEntriesAfterWriteFailure(t *testing.T) {
	isolateInputHistory(t)
	m := &Model{Input: components.NewInput(80)}
	// Temporarily block the history file with a directory.
	path := inputHistoryPath()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	m.rememberInput("pending")
	if got := m.Input.History(); !slices.Equal(got, []string{"pending"}) {
		t.Fatalf("in-memory entries = %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other := &Model{Input: components.NewInput(80)}
	other.rememberInput("other window")
	m.rememberInput("next command")
	want := []string{"other window", "pending", "next command"}
	if got := loadInputHistory(); !slices.Equal(got, want) {
		t.Fatalf("saved history after retry = %q, want %q", got, want)
	}
}
