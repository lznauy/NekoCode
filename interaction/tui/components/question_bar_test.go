package components

import (
	"strings"
	"testing"

	"nekocode/interaction/tui/styles"
	controlruntime "nekocode/runtime"

	"charm.land/lipgloss/v2"
)

func newWrappedQuestionBar() *QuestionBar {
	sty := styles.DefaultStyles()
	qb := NewQuestionBar(&sty)
	qb.SetRequest(&controlruntime.QuestionRequest{
		Questions: []controlruntime.QuestionItem{{
			Header:   "loaded 标记的取舍",
			Question: "保证前缀稳定需要把 `[loaded]` 这个会话可变标记从 Layer 0 移出。你想怎么做？",
			Options: []controlruntime.QuestionOption{
				{Label: "A. 删除标记", Description: "推荐。列表成为 registry+window 的纯函数，恢复字节一致；代价：模型不再从列表看到哪些 skill 已加载（skill 工具 Description 已让模型「已存在就不要再加载」）。"},
				{Label: "B. 标记移到尾部追加消息", Description: "Layer 0 保持纯静态，同时保留模型可见的已加载集合；可借鉴现有 `<runtime_context mode=\"replace\">` 的替换快照语义。"},
				{Label: "C. 恢复时沿用存盘文本不重渲染", Description: "改动最小，但列表可能陈旧。"},
			},
			Custom: true,
		}},
	}, nil)
	return qb
}

func TestQuestionBarWrapsOptionTextWithinWidth(t *testing.T) {
	qb := newWrappedQuestionBar()
	width := 100
	view := qb.View(width, 60)
	barW := width - 4
	for i, line := range strings.Split(strings.TrimRight(view, "\n"), "\n") {
		if w := lipgloss.Width(line); w > barW {
			t.Fatalf("line %d overflows bar width (%d > %d):\n%s", i, w, barW, view)
		}
	}
	for _, want := range []string{"纯函数", "替换快照语义", "改动最小", "(custom)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("wrapped view missing %q:\n%s", want, view)
		}
	}
}

func TestQuestionBarCursorTracksWrappedCustomInput(t *testing.T) {
	qb := newWrappedQuestionBar()
	// Move focus onto the custom row (question + 3 options before it).
	qb.Move(1)
	qb.Move(1)
	qb.Move(1)
	if !qb.CustomActive() {
		t.Fatal("custom row should be active after cycling to it")
	}

	width, termHeight := 80, 40
	qb.Type("自定义回答内容")
	if got := qb.custom[0]; got != "自定义回答内容" {
		t.Fatalf("custom value = %q", got)
	}

	c := qb.Cursor(width, termHeight)
	if c == nil {
		t.Fatal("cursor should be placed on the custom input row")
	}
	view := qb.View(width, termHeight)
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if c.Y < 1 || c.Y >= len(lines) {
		t.Fatalf("cursor row %d outside view (%d lines)", c.Y, len(lines))
	}
	// Cursor column must fall on the custom row's text area, right after the
	// typed value.
	want := lipgloss.Width("  › (custom) ") + lipgloss.Width("自定义回答内容")
	if c.X != want {
		t.Fatalf("cursor X = %d, want %d", c.X, want)
	}

	// Long custom value wraps; cursor stays within view bounds (use a tall
	// terminal so the height cap does not truncate the custom row away).
	qb.Type(strings.Repeat("很长", 60))
	c = qb.Cursor(width, 90)
	if c == nil {
		t.Fatal("cursor should still be placed with wrapped custom value")
	}
	wrappedView := qb.View(width, 90)
	wrappedLines := strings.Split(strings.TrimRight(wrappedView, "\n"), "\n")
	if c.Y >= len(wrappedLines) {
		t.Fatalf("cursor row %d outside view", c.Y)
	}

	// Moving the cursor left keeps coordinates consistent.
	qb.MoveCustomCursor(-5)
	c = qb.Cursor(width, 90)
	if c == nil {
		t.Fatal("cursor should be placed after moving left")
	}
}

func TestQuestionBarCustomEditAtCursor(t *testing.T) {
	sty := styles.DefaultStyles()
	qb := NewQuestionBar(&sty)
	qb.SetRequest(&controlruntime.QuestionRequest{
		Questions: []controlruntime.QuestionItem{{
			Question: "q",
			Options:  []controlruntime.QuestionOption{{Label: "a"}},
			Custom:   true,
		}},
	}, nil)
	qb.Move(1) // focus custom row

	qb.Type("hello")
	qb.MoveCustomCursor(-2)
	qb.Type("X")
	if got := qb.custom[0]; got != "helXlo" {
		t.Fatalf("insert at cursor: got %q", got)
	}
	qb.Backspace()
	if got := qb.custom[0]; got != "hello" {
		t.Fatalf("backspace at cursor: got %q", got)
	}
	qb.MoveCustomCursor(-2)
	qb.DeleteForward()
	if got := qb.custom[0]; got != "hllo" {
		t.Fatalf("delete forward: got %q", got)
	}
	// Deleting forward must leave the position on the rune that shifted into
	// the deleted slot, not drift to the end of the value.
	if got := qb.customPos[0]; got != 1 {
		t.Fatalf("delete forward position: got %d, want 1", got)
	}
	qb.Type("Y")
	if got := qb.custom[0]; got != "hYllo" {
		t.Fatalf("insert after delete forward: got %q", got)
	}
	qb.Backspace()
	if got := qb.custom[0]; got != "hllo" {
		t.Fatalf("backspace after delete forward: got %q", got)
	}
	qb.CustomCursorHome()
	qb.Type(">")
	if got := qb.custom[0]; got != ">hllo" {
		t.Fatalf("home insert: got %q", got)
	}
	qb.CustomCursorEnd()
	qb.Type("<")
	if got := qb.custom[0]; got != ">hllo<" {
		t.Fatalf("end insert: got %q", got)
	}
}

// An editing position that lands exactly on a soft-wrap boundary must point at
// the row where the following rune is rendered, otherwise the caret jumps to
// the end of the previous line.
func TestQuestionBarCursorOnWrapBoundary(t *testing.T) {
	sty := styles.DefaultStyles()
	qb := NewQuestionBar(&sty)
	qb.SetRequest(&controlruntime.QuestionRequest{
		Questions: []controlruntime.QuestionItem{{
			Question: "q",
			Options:  []controlruntime.QuestionOption{{Label: "a"}},
			Custom:   true,
		}},
	}, nil)
	qb.Move(1) // focus custom row

	// width 40 gives the custom row 40-13 = 27 columns, so this 30-rune value
	// wraps after its 27th rune.
	const width, termHeight = 40, 60
	const value = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcd"
	qb.Type(value)

	lines := strings.Split(strings.TrimRight(qb.View(width, termHeight), "\n"), "\n")
	firstRow, tailRow := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "(custom)") {
			firstRow = i
		}
		if strings.Contains(line, "bcd") {
			tailRow = i
		}
	}
	if firstRow < 0 || tailRow <= firstRow {
		t.Fatalf("wrapped custom row not found: first=%d tail=%d\n%s", firstRow, tailRow, strings.Join(lines, "\n"))
	}
	prefixW := lipgloss.Width("  › (custom) ")

	// Boundary: the 27th rune starts the second display row.
	qb.customPos[0] = 27
	if c := qb.Cursor(width, termHeight); c == nil || c.Y != tailRow || c.X != prefixW {
		t.Fatalf("boundary cursor = %+v, want row %d col %d", c, tailRow, prefixW)
	}
	// One rune earlier stays on the first row.
	qb.customPos[0] = 26
	if c := qb.Cursor(width, termHeight); c == nil || c.Y != firstRow {
		t.Fatalf("pre-boundary cursor = %+v, want row %d", c, firstRow)
	}
	// End of text sits after the last rune on the final row.
	qb.customPos[0] = len(value)
	if c := qb.Cursor(width, termHeight); c == nil || c.Y != tailRow || c.X != prefixW+3 {
		t.Fatalf("end cursor = %+v, want row %d col %d", c, tailRow, prefixW+3)
	}
}
