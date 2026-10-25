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
