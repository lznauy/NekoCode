package components

import (
	"math"
	"strings"
	"testing"

	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/styles"
)

func TestHistoryScrollbarStaysContinuousAcrossWindows(t *testing.T) {
	sty := styles.DefaultStyles()
	m := NewMessages(80, 10, &sty)
	items := make([]message.ChatMessage, 100)
	for i := range items {
		items[i] = message.ChatMessage{Role: "user", Content: strings.Repeat("line\n", 3)}
	}
	m.ReplaceHistoryWindow(items, 9901, 10000, 10000, true, false)
	_, _, newest := m.ScrollbarMetrics()
	if newest != 1 {
		t.Fatalf("newest history must reach the end: %v", newest)
	}
	m.ScrollToTop()
	_, _, boundary := m.ScrollbarMetrics()
	if math.Abs(boundary-0.99) > 1e-9 {
		t.Fatalf("top of newest window reset to local progress: %v", boundary)
	}
	m.ReplaceHistoryWindow(items, 9801, 9900, 10000, true, true)
	_, _, older := m.ScrollbarMetrics()
	if math.Abs(older-boundary) > 1e-9 {
		t.Fatalf("loading an adjacent older window jumped: %v -> %v", boundary, older)
	}
	m.ScrollToTop()
	_, _, olderTop := m.ScrollbarMetrics()
	if olderTop >= older || math.Abs(olderTop-0.98) > 1e-9 {
		t.Fatalf("scrolling older must advance within its global range: %v", olderTop)
	}
	// Returning to the newer page starts at its top, at the same boundary.
	m.ReplaceHistoryWindow(items, 9901, 10000, 10000, true, false)
	m.ScrollToTop()
	_, _, newerTop := m.ScrollbarMetrics()
	if math.Abs(newerTop-older) > 1e-9 {
		t.Fatalf("returning newer jumped: %v -> %v", older, newerTop)
	}
	m.ReplaceHistoryWindow(items, 1, 100, 10000, false, true)
	m.ScrollToTop()
	_, _, oldest := m.ScrollbarMetrics()
	if oldest != 0 {
		t.Fatalf("oldest history must reach the beginning: %v", oldest)
	}
}

func TestShortHistoryWindowStillShowsGlobalScrollbar(t *testing.T) {
	sty := styles.DefaultStyles()
	m := NewMessages(80, 30, &sty)
	m.ReplaceHistoryWindow([]message.ChatMessage{{Role: "user", Content: "short page"}}, 50, 50, 100, true, true)
	total, viewport, percent := m.ScrollbarMetrics()
	bar := NewScrollbar(&sty)
	bar.Update(total, viewport, percent)
	if total <= viewport || bar.ViewHorizontal(80) == "" || percent != 0.5 {
		t.Fatalf("short window lost its global position: height=%d viewport=%d position=%v", total, viewport, percent)
	}
	m.ReplaceMessages([]message.ChatMessage{{Role: "user", Content: strings.Repeat("new session\n", 100)}})
	total, _, percent = m.ScrollbarMetrics()
	if total != m.TotalContentHeight() || percent != m.List.ScrollPercent() {
		t.Fatal("session replacement retained previous history coordinates")
	}
}
