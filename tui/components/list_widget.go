// ListWidget 通用滚动列表组件：管理 Item 集合、视口滚动、鼠标滚轮支持。
package components

import (
	"strings"
	"sync"
)

type Item interface {
	Render(width int) string
	Height(width int) int
}

type renderedItem struct {
	lines  []string
	height int
}

type List struct {
	width, height int
	items         []Item
	gap           int

	offsetIdx  int
	offsetLine int

	cache       map[int]renderedItem
	cacheMu     sync.RWMutex
	cacheWid    int
	totalHeight int  // cached total content height, -1 if dirty
	pixelsAbove int  // cumulative pixels scrolled above viewport, -1 if dirty
}

func NewList(items ...Item) *List {
	return &List{
		items:       items,
		cache:       make(map[int]renderedItem),
		totalHeight: -1,
		pixelsAbove: -1,
	}
}

func (l *List) SetSize(width, height int) {
	l.width = width
	if height < 0 {
		height = 0
	}
	l.height = height
}

func (l *List) SetGap(gap int) { l.gap = gap }

func (l *List) Width() int  { return l.width }
func (l *List) Height() int { return l.height }
func (l *List) Len() int    { return len(l.items) }

func (l *List) Items() []Item { return l.items }

func (l *List) SetItems(items ...Item) {
	l.items = items
	l.offsetIdx = 0
	l.offsetLine = 0
	l.totalHeight = -1
	l.pixelsAbove = 0
	l.clearCache()
}

func (l *List) AppendItems(items ...Item) {
	l.items = append(l.items, items...)
	l.totalHeight = -1
	l.pixelsAbove = -1
}

func (l *List) getItem(idx int) renderedItem {
	if idx < 0 || idx >= len(l.items) {
		return renderedItem{}
	}

	l.cacheMu.RLock()
	if l.cacheWid == l.width {
		if cached, ok := l.cache[idx]; ok {
			l.cacheMu.RUnlock()
			return cached
		}
	}
	l.cacheMu.RUnlock()

	item := l.items[idx]
	content := item.Render(l.width)
	content = strings.TrimRight(content, "\n")
	lines := strings.Split(content, "\n")
	height := len(lines)

	ri := renderedItem{lines: lines, height: height}

	l.cacheMu.Lock()
	if l.cacheWid != l.width {
		l.cache = make(map[int]renderedItem)
		l.cacheWid = l.width
	}
	l.cache[idx] = ri
	l.cacheMu.Unlock()

	return ri
}

func (l *List) clearCache() {
	l.cacheMu.Lock()
	l.cache = make(map[int]renderedItem)
	l.totalHeight = -1
	l.pixelsAbove = -1
	l.cacheMu.Unlock()
}

func (l *List) InvalidateItem(idx int) {
	l.cacheMu.Lock()
	old, hadOld := l.cache[idx]
	delete(l.cache, idx)
	l.cacheMu.Unlock()

	if hadOld {
		newItem := l.getItem(idx)
		l.cacheMu.Lock()
		if l.totalHeight >= 0 {
			l.totalHeight += newItem.height - old.height
		}
		if idx < l.offsetIdx && l.pixelsAbove >= 0 {
			l.pixelsAbove += newItem.height - old.height
		}
		l.cacheMu.Unlock()
	} else {
		l.cacheMu.Lock()
		l.totalHeight = -1
		l.pixelsAbove = -1
		l.cacheMu.Unlock()
	}
}

func (l *List) Invalidate() { l.clearCache() }

// recomputePixelsAbove rebuilds pixelsAbove from offsetIdx/offsetLine.
// Called after scroll operations, not per-frame.
func (l *List) recomputePixelsAbove() {
	l.pixelsAbove = l.offsetLine
	for i := 0; i < l.offsetIdx; i++ {
		it := l.getItem(i)
		l.pixelsAbove += it.height
		if l.gap > 0 {
			l.pixelsAbove += l.gap
		}
	}
}

func (l *List) AtBottom() bool {
	if len(l.items) == 0 {
		return true
	}
	th := l.TotalContentHeight()
	if th <= l.height {
		return true
	}
	if l.pixelsAbove < 0 {
		l.recomputePixelsAbove()
	}
	return l.pixelsAbove+l.height >= th
}

func (l *List) ScrollToTop() {
	l.offsetIdx = 0
	l.offsetLine = 0
	l.pixelsAbove = 0
}

func (l *List) ScrollToBottom() {
	if len(l.items) == 0 {
		return
	}
	th := l.TotalContentHeight()
	if th <= l.height {
		l.offsetIdx = 0
		l.offsetLine = 0
		l.pixelsAbove = 0
		return
	}
	remaining := l.height
	idx := len(l.items) - 1
	for idx >= 0 && remaining > 0 {
		it := l.getItem(idx)
		h := it.height
		if l.gap > 0 && idx < len(l.items)-1 {
			h += l.gap
		}
		remaining -= h
		if remaining >= 0 {
			idx--
		}
	}
	l.offsetIdx = max(idx, 0)
	above := 0
	for i := 0; i < l.offsetIdx; i++ {
		it := l.getItem(i)
		above += it.height
		if l.gap > 0 {
			above += l.gap
		}
	}
	l.offsetLine = max(th-l.height-above, 0)
	l.pixelsAbove = above + l.offsetLine
}

func (l *List) ScrollBy(lines int) {
	if len(l.items) == 0 || lines == 0 {
		return
	}

	if lines > 0 {
		if l.AtBottom() {
			return
		}

		l.offsetLine += lines
		currentItem := l.getItem(l.offsetIdx)
		for l.offsetLine >= currentItem.height {
			l.offsetLine -= currentItem.height
			if l.gap > 0 {
				l.offsetLine = max(0, l.offsetLine-l.gap)
			}

			l.offsetIdx++
			if l.offsetIdx > len(l.items)-1 {
				l.ScrollToBottom()
				return
			}
			currentItem = l.getItem(l.offsetIdx)
		}

		// pixelsAbove tracks cumulative scroll; lines already includes any consumed gaps.
		l.pixelsAbove += lines
		th := l.TotalContentHeight()
		if th > l.height && l.pixelsAbove > th-l.height {
			l.pixelsAbove = th - l.height
		}
	} else {
		l.offsetLine += lines
		for l.offsetLine < 0 {
			l.offsetIdx--
			if l.offsetIdx < 0 {
				l.ScrollToTop()
				return
			}
			prevItem := l.getItem(l.offsetIdx)
			totalHeight := prevItem.height
			if l.gap > 0 {
				totalHeight += l.gap
			}
			l.offsetLine += totalHeight
		}

		l.pixelsAbove += lines
		if l.pixelsAbove < 0 {
			l.pixelsAbove = 0
		}
	}
}


func (l *List) Render() string {
	if len(l.items) == 0 {
		return ""
	}

	var lines []string
	currentIdx := l.offsetIdx
	currentOffset := l.offsetLine
	linesNeeded := l.height

	for linesNeeded > 0 && currentIdx < len(l.items) {
		item := l.getItem(currentIdx)
		itemLines := item.lines
		itemHeight := item.height

		if currentOffset >= 0 && currentOffset < itemHeight {
			startLine := currentOffset
			if startLine < len(itemLines) {
				lines = append(lines, itemLines[startLine:]...)
			}

			if l.gap > 0 {
				for i := 0; i < l.gap && len(lines) < l.height; i++ {
					lines = append(lines, "")
				}
			}
		} else if currentOffset >= itemHeight && l.gap > 0 {
			gapOffset := currentOffset - itemHeight
			gapRemaining := l.gap - gapOffset
			for i := 0; i < gapRemaining && len(lines) < l.height; i++ {
				lines = append(lines, "")
			}
		}

		linesNeeded = l.height - len(lines)
		currentIdx++
		currentOffset = 0
	}

	if l.height <= 0 {
		return ""
	}
	if len(lines) > l.height {
		lines = lines[:l.height]
	}

	return strings.Join(lines, "\n")
}

func (l *List) TotalContentHeight() int {
	if l.totalHeight < 0 {
		var total int
		for i := 0; i < len(l.items); i++ {
			item := l.getItem(i)
			total += item.height
			if l.gap > 0 && i > 0 {
				total += l.gap
			}
		}
		l.totalHeight = total
	}
	return l.totalHeight
}

// ScrollY returns the current scroll offset in pixels (lines above viewport).
func (l *List) ScrollY() int {
	if l.pixelsAbove < 0 {
		l.recomputePixelsAbove()
	}
	return l.pixelsAbove
}

// ScrollPercent returns the scroll position as a fraction [0, 1].
func (l *List) ScrollPercent() float64 {
	if len(l.items) == 0 {
		return 0
	}
	totalHeight := l.TotalContentHeight()
	if totalHeight <= l.height {
		return 0
	}

	maxOffset := totalHeight - l.height
	if maxOffset <= 0 {
		return 0
	}

	if l.pixelsAbove < 0 {
		l.recomputePixelsAbove()
	}

	return float64(l.pixelsAbove) / float64(maxOffset)
}
