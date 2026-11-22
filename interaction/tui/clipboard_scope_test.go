package tui

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type clipboardScopeRuntime struct {
	tickFakeBot
	session string
}

func (r *clipboardScopeRuntime) CurrentSessionID() string      { return r.session }
func (r *clipboardScopeRuntime) ImageAttachmentsEnabled() bool { return true }

func TestClipboardCommandCapturesScopeBeforeExecution(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses WSL's unavailable-interoperability result without accessing a real clipboard")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("PATH", t.TempDir())
	r := &clipboardScopeRuntime{session: "session-A"}
	m, err := NewModel(r)
	if err != nil {
		t.Fatal(err)
	}
	scope := m.clipboardScope()
	cmd := m.handleKeyPress(tea.KeyPressMsg{Code: 'v', Mod: tea.ModAlt})
	r.session = "session-B"
	msg := cmd().(clipboardImageMsg)
	if msg.scope != scope || msg.err == nil {
		t.Fatal("asynchronous command lost its originating scope")
	}
	m.Update(msg)
	if len(m.Messages.Items()) != 0 {
		t.Fatal("late clipboard failure appeared in another session")
	}
}

func TestClipboardResultsStayWithOriginalDraft(t *testing.T) {
	for _, payload := range []string{"image", "text", "error"} {
		for _, change := range []string{"session", "session_round_trip", "clear", "submit", "replace", "history_round_trip", "unchanged", "typing"} {
			t.Run(payload+"/"+change, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				r := &clipboardScopeRuntime{session: "session-A"}
				m, err := NewModel(r)
				if err != nil {
					t.Fatal(err)
				}
				m.Input.SetValue("original ")
				m.Input.SetHistory([]string{"previous"})
				// The result keeps exactly the scope captured when the key starts a read.
				scope := m.clipboardScope()
				if m.handleKeyPress(tea.KeyPressMsg{Code: 'v', Mod: tea.ModAlt}) == nil {
					t.Fatal("paste did not start")
				}
				var imageData bytes.Buffer
				if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
					t.Fatal(err)
				}
				msg := clipboardImageMsg{scope: scope}
				switch payload {
				case "image":
					msg.data = imageData.Bytes()
				case "text":
					msg.text = "pasted text"
				case "error":
					msg.err = errors.New("clipboard failure")
				}
				switch change {
				case "session":
					r.session = "session-B"
				case "session_round_trip":
					r.session = "session-B"
					m.loadSessionMessages()
					r.session = "session-A"
					m.loadSessionMessages()
				case "clear":
					m.Input.Clear()
				case "submit":
					m.startChat(m.Input.Value(), nil)
				case "replace":
					m.Input.SetValue("replacement draft")
				case "history_round_trip":
					m.Input.HistoryUp()
					m.Input.HistoryDown()
				case "typing":
					m.Input.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
				}
				beforeText := m.Input.Value()
				beforeMessages := len(m.Messages.Items())
				// Route through Update, including text fallback, rather than bypassing it.
				m.Update(msg)
				valid := change == "unchanged" || change == "typing"
				if !valid {
					if len(m.Input.ImageAttachments()) != 0 || m.Input.Value() != beforeText || len(m.Messages.Items()) != beforeMessages {
						t.Fatal("stale clipboard result changed another draft")
					}
					dir := os.Getenv("HOME") + "/.nekocode/tmp/images"
					if _, err := os.Stat(dir); !os.IsNotExist(err) {
						t.Fatalf("stale result created attachment storage: %v", err)
					}
					return
				}
				switch payload {
				case "image":
					if len(m.Input.ImageAttachments()) != 1 {
						t.Fatal("current paste lost")
					}
				case "text":
					if m.Input.Value() == beforeText {
						t.Fatal("current text paste lost")
					}
				case "error":
					if len(m.Messages.Items()) != beforeMessages+1 {
						t.Fatal("current error lost")
					}
				}
			})
		}
	}
}

func TestClipboardResultCannotChangeImageDraftAwaitingAcceptance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := &clipboardScopeRuntime{session: "session-A"}
	m, err := NewModel(r)
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	m.handleClipboardImage(clipboardImageMsg{scope: m.clipboardScope(), data: data.Bytes()})
	before := m.Input.ImageAttachments()
	if len(before) != 1 {
		t.Fatal("initial attachment missing")
	}
	scope := m.clipboardScope()
	m.startChat(m.Input.Value(), inputImageAttachments(before))
	m.Update(clipboardImageMsg{scope: scope, data: data.Bytes()})
	if m.Input.HasContent() || m.queue.awaiting == nil || m.queue.awaiting.Images()[0].Path != before[0].Path {
		t.Fatal("late paste changed submitted image draft")
	}
}
