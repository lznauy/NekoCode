package components

import (
	"strings"
	"testing"
)

type fixedHeightListItem struct{ height int }

func (i fixedHeightListItem) Render(int) string { return strings.Repeat("item\n", i.height) }
func (i fixedHeightListItem) Height(int) int    { return i.height }

type countingListItem struct{ height, renders int }

func (i *countingListItem) Render(int) string { i.renders++; return strings.Repeat("item\n", i.height) }
func (i *countingListItem) Height(int) int    { return i.height }

func TestReplaceItemPreservesOtherRendersAndViewport(t *testing.T) {
	first, second := &countingListItem{height: 1}, &countingListItem{height: 1}
	list := NewList(first, second)
	list.SetSize(80, 1)
	list.TotalContentHeight()
	list.ScrollBy(1)
	replacement := &countingListItem{height: 3}
	list.ReplaceItem(0, replacement)
	list.Render()
	index, line := list.ViewportAnchor()
	if index != 1 || line != 0 || list.ScrollY() != 3 || list.TotalContentHeight() != 4 {
		t.Fatalf("replacement lost viewport or height: anchor=%d,%d scroll=%d total=%d", index, line, list.ScrollY(), list.TotalContentHeight())
	}
	if first.renders != 1 || second.renders != 1 || replacement.renders != 1 {
		t.Fatal("replacement invalidated unchanged item renders")
	}
}

func TestScrollToBottomUsesCanonicalItemAnchor(t *testing.T) {
	list := NewList(fixedHeightListItem{height: 1}, fixedHeightListItem{height: 1})
	list.SetSize(80, 1)
	list.ScrollToBottom()
	index, line := list.ViewportAnchor()
	if index != 1 || line != 0 {
		t.Fatalf("bottom anchor points past preceding item: %d,%d", index, line)
	}
}

func TestScrollUpAfterRestoreViewportAnchorKeepsPosition(t *testing.T) {
	items := make([]Item, 5)
	for i := range items {
		items[i] = fixedHeightListItem{height: 3}
	}
	list := NewList(items...)
	list.SetSize(80, 3)
	list.RestoreViewportAnchor(2, 1)
	list.ScrollBy(-1)
	index, line := list.ViewportAnchor()
	if index != 2 || line != 0 {
		t.Fatalf("scroll up jumped from restored anchor: index=%d line=%d", index, line)
	}
	list.ScrollBy(-1)
	index, line = list.ViewportAnchor()
	if index != 1 || line != 2 {
		t.Fatalf("second scroll up lost position: index=%d line=%d", index, line)
	}
}

type reflowListItem struct{}

func (reflowListItem) Render(width int) string {
	return strings.TrimSuffix(strings.Repeat("visible line\n", max(1, 400/max(width, 1))), "\n")
}
func (i reflowListItem) Height(width int) int { return strings.Count(i.Render(width), "\n") + 1 }

func TestResizeClampsReflowedViewport(t *testing.T) {
	list := NewList(reflowListItem{})
	list.SetSize(20, 3)
	list.ScrollBy(12)
	list.SetSize(100, 3)
	if !strings.Contains(list.Render(), "visible line") {
		t.Fatal("widening the viewport made the current item disappear")
	}
	if list.ScrollPercent() > 1 {
		t.Fatalf("scroll percentage after reflow = %v", list.ScrollPercent())
	}
	if got := len(strings.Split(list.Render(), "\n")); got != 3 {
		t.Fatalf("reflow left avoidable empty space: %d lines", got)
	}
}

func TestTallerViewportClampsToRealBottom(t *testing.T) {
	list := NewList(fixedHeightListItem{height: 10})
	list.SetSize(80, 3)
	list.ScrollToBottom()
	list.SetSize(80, 8)
	if got := len(strings.Split(list.Render(), "\n")); got != 8 {
		t.Fatalf("growing viewport left avoidable empty space: %d lines", got)
	}
	if list.ScrollPercent() > 1 {
		t.Fatalf("scroll percentage after resize = %v", list.ScrollPercent())
	}
}

func TestScrollByPreservesInterMessageGaps(t *testing.T) {
	for _, gap := range []int{1, 2} {
		list := NewList(fixedHeightListItem{height: 3}, fixedHeightListItem{height: 3})
		list.SetGap(gap)
		list.SetSize(80, 2)
		reference := []string{"item", "item", "item"}
		reference = append(reference, make([]string, gap)...)
		reference = append(reference, "item", "item", "item")
		wantOffset := 0
		for _, delta := range []int{1, 1, 1, 1, 1, 1, -1, -1, -1, -1, -1, -1, 3, -1, 2, -3} {
			list.ScrollBy(delta)
			wantOffset = max(0, min(wantOffset+delta, len(reference)-2))
			want := strings.Join(reference[wantOffset:wantOffset+2], "\n")
			if got := list.Render(); got != want {
				t.Fatalf("gap %d, delta %d, offset %d: rendered %q, want %q", gap, delta, wantOffset, got, want)
			}
			if list.ScrollY() != wantOffset {
				t.Fatalf("gap %d: offset %d, want %d", gap, list.ScrollY(), wantOffset)
			}
		}
	}
}

func TestGapViewportAnchorSurvivesRestoreAndResize(t *testing.T) {
	list := NewList(fixedHeightListItem{height: 3}, fixedHeightListItem{height: 3})
	list.SetGap(1)
	list.SetSize(80, 2)
	list.ScrollBy(3)
	index, line := list.ViewportAnchor()
	before := list.Render()
	list.RestoreViewportAnchor(index, line)
	if list.Render() != before || list.ScrollY() != 3 {
		t.Fatal("restoring an anchor inside a gap changed the viewport")
	}
	list.SetSize(100, 2)
	if list.Render() != before || list.ScrollY() != 3 {
		t.Fatal("width-only resize changed a still-valid gap anchor")
	}
}

func TestShrinkingLiveItemKeepsViewportValid(t *testing.T) {
	live := &countingListItem{height: 20}
	list := NewList(fixedHeightListItem{height: 20}, live)
	list.SetSize(80, 3)
	list.ScrollToBottom()
	list.ScrollBy(-5)
	live.height = 3
	list.InvalidateItem(1)
	if got := list.Render(); got == "" || len(strings.Split(got, "\n")) != 3 {
		t.Fatalf("shrinking live item left a stale viewport: %q", got)
	}
	if list.ScrollPercent() > 1 {
		t.Fatalf("shrink left scroll beyond bottom: %v", list.ScrollPercent())
	}
}
