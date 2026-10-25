package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSessionLoadHintDelayAndCompletion(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			m, err := NewModel(&windowSessionRuntime{})
			if err != nil {
				t.Fatal(err)
			}
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
			m.applySessionLoaded(m.loadSessionMessages()().(sessionLoadedMsg))
			m.Messages.ScrollToTop()
			load := m.loadSessionWindow(9901, 1)
			started := m.sessionLoadStarted
			normal := m.Header.View()
			if got := m.sessionHeaderView(started.Add(sessionLoadHintDelay - time.Nanosecond)); got != normal {
				t.Fatal("fast history reads must not flash a header hint")
			}
			scrollY, height := m.Messages.ScrollY(), m.Messages.Height()
			slow := m.sessionHeaderView(started.Add(sessionLoadHintDelay))
			if !strings.Contains(ansi.Strip(slow), "正在加载更早消息…") {
				t.Fatalf("slow history read has no visible top hint: %q", slow)
			}
			if lipgloss.Height(slow) != lipgloss.Height(normal) || m.Messages.Height() != height || m.Messages.ScrollY() != scrollY {
				t.Fatal("showing the hint changed the viewport or reading position")
			}
			loaded := load().(sessionLoadedMsg)
			if fail {
				loaded.err = errors.New("history unavailable")
			}
			m.Update(loaded)
			if strings.Contains(ansi.Strip(m.sessionHeaderView(started.Add(time.Second))), "正在加载") {
				t.Fatal("completion left the loading hint visible")
			}
		})
	}
}

func TestSessionLoadHintResetsForNewRequest(t *testing.T) {
	m, err := NewModel(&windowSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	old := m.loadSessionWindow(9901, 1)().(sessionLoadedMsg)
	m.sessionLoadStarted = time.Now().Add(-time.Second)
	if m.sessionHeaderView(time.Now()) == m.Header.View() {
		t.Fatal("test did not start with a visible hint")
	}
	m.sessionHistoryPage = 1
	load := m.loadSessionWindow(0, 0)
	started := m.sessionLoadStarted
	m.Update(old)
	if !m.sessionLoading || m.sessionHeaderView(started) != m.Header.View() {
		t.Fatal("new request inherited the hint or stale completion ended it")
	}
	if !strings.Contains(ansi.Strip(m.sessionHeaderView(started.Add(sessionLoadHintDelay))), "正在加载较新消息…") {
		t.Fatal("newer history load has the wrong direction label")
	}
	m.Update(load().(sessionLoadedMsg))
	if strings.Contains(ansi.Strip(m.sessionHeaderView(started.Add(time.Second))), "正在加载") {
		t.Fatal("fast completion produced a delayed hint")
	}
}

func TestSessionLoadHintFitsNarrowHeader(t *testing.T) {
	m, err := NewModel(&windowSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 10, Height: 20})
	m.loadSessionWindow(9901, 1)
	header := m.sessionHeaderView(m.sessionLoadStarted.Add(sessionLoadHintDelay))
	lines := strings.Split(header, "\n")
	if len(lines) != 3 || ansi.StringWidth(lines[1]) > 10 {
		t.Fatalf("hint wrapped or overflowed the header: %q", header)
	}
}
