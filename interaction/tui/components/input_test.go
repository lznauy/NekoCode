package components

import (
	"fmt"
	"strings"
	"testing"

	"nekocode/interaction/tui/styles"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestInputWrapsLongTextWithoutHidingPrefix(t *testing.T) {
	in := NewInput(32)
	initialHeight := in.Height()
	in.SetValue("alpha-beta-gamma-delta-epsilon-zeta-eta-theta-iota-kappa")
	in.SetCursorEnd()

	view := in.View()
	clean := ansi.Strip(view)
	if !strings.Contains(clean, "alpha-beta") {
		t.Fatalf("wrapped input should keep the prefix visible:\n%s", clean)
	}
	if !strings.Contains(clean, "kappa") {
		t.Fatalf("wrapped input should keep the suffix visible:\n%s", clean)
	}
	if strings.Contains(clean, "alpha-beta-gamma-delta-epsilon-zeta-eta-theta-iota-kappa") {
		t.Fatalf("input should wrap instead of staying on one line:\n%s", clean)
	}
	if in.Height() <= initialHeight {
		t.Fatalf("input height should grow for wrapped text, got %d <= %d", in.Height(), initialHeight)
	}
	for _, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 32 {
			t.Fatalf("input line width = %d, want <= 32:\n%s", w, clean)
		}
	}
}

func TestInputFooterShowsModelReasoningEffort(t *testing.T) {
	in := NewInput(120)
	in.SetPermissionMode("manual")
	in.SetModel("deepseek/deepseek-v4-flash")
	in.SetReasoningEffort("medium")
	clean := ansi.Strip(in.View())
	want := "Follow: Auto · Perm: Manual · Effort: medium · Model: deepseek/deepseek-v4-flash"
	if !strings.Contains(clean, want) {
		t.Fatalf("footer = %q, want %q", clean, want)
	}
	if !strings.Contains(in.View(), styles.TealStyle.Render("Manual")) || !strings.Contains(in.View(), styles.YellowStyle.Render("medium")) {
		t.Fatalf("footer semantic colors were not applied:\n%s", in.View())
	}
}

func TestInputFooterShowsAutoForUnsetEffortAndFitsNarrowWidth(t *testing.T) {
	in := NewInput(42)
	in.SetModel("deepseek/deepseek-v4-flash")
	view := in.View()
	if !strings.Contains(ansi.Strip(view), "E:Auto") {
		t.Fatalf("unset effort was not shown as Auto:\n%s", ansi.Strip(view))
	}
	for _, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width > 42 {
			t.Fatalf("footer line width = %d, want <= 42:\n%s", width, ansi.Strip(view))
		}
	}
}

func TestInputClearDropsDraftAndDetachesHistoryNavigation(t *testing.T) {
	in := NewInput(80)
	in.SetHistory([]string{"first", "second"})
	in.HistoryUp()
	if got := in.Value(); got != "second" {
		t.Fatalf("history value = %q, want second", got)
	}

	in.Clear()
	if in.HasContent() || in.Value() != "" {
		t.Fatalf("input remained populated after clear: %q", in.Value())
	}
	in.HistoryDown()
	if got := in.Value(); got != "" {
		t.Fatalf("history down restored cleared draft: %q", got)
	}
	in.HistoryUp()
	if got := in.Value(); got != "second" {
		t.Fatalf("history up after clear = %q, want second", got)
	}
}

func TestInputSetValueDetachesHistoryNavigationAndAllowsImages(t *testing.T) {
	in := NewInput(80)
	var removed []string
	in.SetImageCleanup(func(path string) { removed = append(removed, path) })
	in.SetHistory([]string{"older"})
	if !in.AddImage("/tmp/original.png") {
		t.Fatal("AddImage original failed")
	}
	in.HistoryUp()

	in.SetValue("replacement")
	if in.BrowsingHistory() {
		t.Fatal("SetValue left history navigation active")
	}
	if len(removed) != 1 || removed[0] != "/tmp/original.png" {
		t.Fatalf("SetValue did not release saved draft image: %#v", removed)
	}
	if !in.AddImage("/tmp/replacement.png") {
		t.Fatal("SetValue left image attachment blocked")
	}

	in.HistoryDown()
	if got := in.Value(); got != "replacement[Image #1]" {
		t.Fatalf("history down replaced detached input: %q", got)
	}
}

func TestInputCollapsesLargePasteWithoutChangingSubmittedValue(t *testing.T) {
	in := NewInput(120)
	content := strings.Repeat("log line with useful details\n", 20) + "final line"

	updated, _ := in.Update(tea.PasteMsg{Content: content})
	in = updated
	clean := ansi.Strip(in.View())
	wantMarker := fmt.Sprintf("[Pasted Content #1: 21 lines, %d chars]", len([]rune(content)))
	if !strings.Contains(clean, wantMarker) {
		t.Fatalf("large paste placeholder missing:\n%s", clean)
	}
	if strings.Contains(clean, "log line with useful details") {
		t.Fatalf("large paste leaked into rendered input:\n%s", clean)
	}
	if got := in.Value(); got != content {
		t.Fatalf("expanded paste differs from original: got %d chars, want %d", len([]rune(got)), len([]rune(content)))
	}
}

func TestInputKeepsShortPasteVisible(t *testing.T) {
	in := NewInput(80)
	updated, _ := in.Update(tea.PasteMsg{Content: "short pasted text"})
	in = updated
	if got := in.Value(); got != "short pasted text" {
		t.Fatalf("short paste value = %q", got)
	}
	if strings.Contains(ansi.Strip(in.View()), "Pasted Content") {
		t.Fatalf("short paste was unexpectedly collapsed:\n%s", ansi.Strip(in.View()))
	}
}

func TestInputImageMarkerSkipsLiteralUserText(t *testing.T) {
	in := NewInput(80)
	in.SetValue("compare with [Image #1] ")
	in.SetCursorEnd()
	if !in.AddImage("/tmp/image.png") {
		t.Fatal("AddImage rejected a valid image")
	}
	images := in.ImageAttachments()
	if len(images) != 1 || images[0].Label != "[Image #2]" {
		t.Fatalf("image attachments = %#v, want collision-free marker", images)
	}
}

func TestInputLargePastePreservesSurroundingText(t *testing.T) {
	in := NewInput(120)
	in.SetValue("before ")
	content := strings.Repeat("x", largePasteCharThreshold)
	in, _ = in.Update(tea.PasteMsg{Content: content})
	in, _ = in.Update(tea.KeyPressMsg(tea.Key{Code: '!', Text: "!"}))

	if got, want := in.Value(), "before "+content+"!"; got != want {
		t.Fatalf("expanded value length = %d, want %d", len([]rune(got)), len([]rune(want)))
	}
}

func TestInputBackspaceRemovesLargePasteAtomically(t *testing.T) {
	in := NewInput(120)
	content := strings.Repeat("x", largePasteCharThreshold)
	in, _ = in.Update(tea.PasteMsg{Content: content})
	in, _ = in.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))

	if in.HasContent() || in.Value() != "" {
		t.Fatalf("backspace left part of paste placeholder: %q", in.Value())
	}
}

func TestInputImagePlaceholderIsAtomicAndCleansDraft(t *testing.T) {
	in := NewInput(120)
	var removed []string
	in.SetImageCleanup(func(path string) { removed = append(removed, path) })
	if !in.AddImage("/tmp/paste.png") {
		t.Fatal("image was not added")
	}
	if got := in.Value(); got != "[Image #1]" {
		t.Fatalf("input value = %q", got)
	}
	if got := in.ImageAttachments(); len(got) != 1 || got[0].Label != "[Image #1]" || got[0].Path != "/tmp/paste.png" {
		t.Fatalf("attachments = %#v", got)
	}

	in, _ = in.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if in.HasContent() || len(in.ImageAttachments()) != 0 {
		t.Fatalf("image placeholder was not removed atomically: %q", in.Value())
	}
	if len(removed) != 1 || removed[0] != "/tmp/paste.png" {
		t.Fatalf("removed paths = %#v", removed)
	}
}

func TestInputResetRetainsSubmittedImageButClearDeletesDraft(t *testing.T) {
	in := NewInput(120)
	var removed []string
	in.SetImageCleanup(func(path string) { removed = append(removed, path) })
	in.AddImage("/tmp/submitted.png")
	in.Reset()
	if len(removed) != 0 {
		t.Fatalf("reset deleted submitted image: %#v", removed)
	}
	in.AddImage("/tmp/draft.png")
	in.Clear()
	if len(removed) != 1 || removed[0] != "/tmp/draft.png" {
		t.Fatalf("clear did not delete draft image: %#v", removed)
	}
}

func TestInputLargePasteStillHonorsExpandedCharacterLimit(t *testing.T) {
	in := NewInput(120)
	content := strings.Repeat("x", charLimit)
	in, _ = in.Update(tea.PasteMsg{Content: content})
	in, _ = in.Update(tea.KeyPressMsg(tea.Key{Code: '!', Text: "!"}))

	if got := in.Value(); got != content {
		t.Fatalf("typing exceeded expanded character limit: got %d chars, want %d", len([]rune(got)), len([]rune(content)))
	}
}

func TestInputHistoryRestoresLargeEntryAsPlaceholder(t *testing.T) {
	content := strings.Repeat("history line\n", 12) + "end"
	in := NewInput(120)
	in.SetHistory([]string{content})
	in.HistoryUp()

	if got := in.Value(); got != content {
		t.Fatalf("restored history differs from original")
	}
	if !strings.Contains(ansi.Strip(in.View()), "[Pasted Content #1:") {
		t.Fatalf("large history entry was not collapsed:\n%s", ansi.Strip(in.View()))
	}
}

func TestInputHistoryPreservesImageDraft(t *testing.T) {
	in := NewInput(120)
	var removed []string
	in.SetImageCleanup(func(path string) { removed = append(removed, path) })
	in.SetHistory([]string{"describe this"})
	if !in.AddImage("/tmp/draft.png") {
		t.Fatal("AddImage failed")
	}

	in.HistoryUp()
	if got := in.Value(); got != "describe this" {
		t.Fatalf("history up value = %q", got)
	}
	if len(removed) != 0 {
		t.Fatalf("history navigation deleted draft images: %#v", removed)
	}

	in.HistoryDown()
	if got := in.Value(); got != "[Image #1]" {
		t.Fatalf("restored draft value = %q", got)
	}
	images := in.ImageAttachments()
	if len(images) != 1 || images[0].Label != "[Image #1]" || images[0].Path != "/tmp/draft.png" {
		t.Fatalf("restored draft images = %#v", images)
	}
	if len(removed) != 0 {
		t.Fatalf("restored draft image was deleted: %#v", removed)
	}
	in.Clear()
	if len(removed) != 1 || removed[0] != "/tmp/draft.png" {
		t.Fatalf("clearing restored draft removed images = %#v", removed)
	}
}

func TestSetHistoryPreservesImageDraftWhileBrowsing(t *testing.T) {
	in := NewInput(120)
	var removed []string
	in.SetImageCleanup(func(path string) { removed = append(removed, path) })
	in.SetHistory([]string{"older"})
	if !in.AddImage("/tmp/draft.png") {
		t.Fatal("AddImage failed")
	}

	in.HistoryUp()
	in.SetHistory([]string{"older", "new entry"})
	if len(removed) != 0 {
		t.Fatalf("history refresh deleted saved draft images: %#v", removed)
	}
	in.HistoryDown()
	if got := in.Value(); got != "[Image #1]" {
		t.Fatalf("restored draft value = %q", got)
	}
	images := in.ImageAttachments()
	if len(images) != 1 || images[0].Path != "/tmp/draft.png" {
		t.Fatalf("restored draft images = %#v", images)
	}
}

func TestInputResetWhileBrowsingHistoryCleansSavedImageDraft(t *testing.T) {
	in := NewInput(120)
	var removed []string
	in.SetImageCleanup(func(path string) { removed = append(removed, path) })
	in.SetHistory([]string{"describe this"})
	if !in.AddImage("/tmp/draft.png") {
		t.Fatal("AddImage failed")
	}

	in.HistoryUp()
	in.Reset()
	if len(removed) != 1 || removed[0] != "/tmp/draft.png" {
		t.Fatalf("reset while browsing history removed images = %#v", removed)
	}
	if in.HasContent() || len(in.ImageAttachments()) != 0 {
		t.Fatalf("reset retained input state: value=%q images=%#v", in.Value(), in.ImageAttachments())
	}
}

func TestInputHistoryRejectsImagesAddedToRecalledEntry(t *testing.T) {
	in := NewInput(120)
	var removed []string
	in.SetImageCleanup(func(path string) { removed = append(removed, path) })
	in.SetHistory([]string{"older"})
	if !in.AddImage("/tmp/original.png") {
		t.Fatal("AddImage original failed")
	}

	in.HistoryUp()
	if in.AddImage("/tmp/recalled.png") {
		t.Fatal("AddImage accepted an attachment while browsing history")
	}
	in.HistoryDown()
	images := in.ImageAttachments()
	if len(images) != 1 || images[0].Path != "/tmp/original.png" {
		t.Fatalf("original draft images = %#v", images)
	}
	if len(removed) != 0 {
		t.Fatalf("history image rejection deleted an attachment: %#v", removed)
	}
}

func TestInputRejectsEditingInsideLargePasteMarkerWithoutLosingContent(t *testing.T) {
	in := NewInput(120)
	content := strings.Repeat("hidden", 300)
	in, _ = in.Update(tea.PasteMsg{Content: content})
	marker := in.pastedBlocks[0].marker
	in.setCursorRuneOffset(len([]rune(marker)) / 2)
	in, _ = in.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))

	if got := in.Value(); got != content {
		t.Fatalf("editing a marker lost hidden content: got %d runes, want %d", len([]rune(got)), len([]rune(content)))
	}
	if got := in.textarea.Value(); got != marker {
		t.Fatalf("marker was mutated: %q", got)
	}
}

func TestInputExpandsOnlyTheBoundPasteMarkerOccurrence(t *testing.T) {
	in := NewInput(120)
	content := strings.Repeat("payload", 200)
	in, _ = in.Update(tea.PasteMsg{Content: content})
	marker := in.pastedBlocks[0].marker
	in.setCursorRuneOffset(0)
	in, _ = in.Update(tea.PasteMsg{Content: marker})

	if got, want := in.Value(), marker+content; got != want {
		t.Fatalf("literal marker was expanded: got %q... (%d runes), want %d runes", truncateRunes(got, 80), len([]rune(got)), len([]rune(want)))
	}
}

func TestSanitizeInputLimitBoundsLargeInputAndNormalizesCRLF(t *testing.T) {
	input := "a\r\n\t" + strings.Repeat("z", charLimit*10)
	got := sanitizeInputLimit(input, 8)
	if got != "a\n    zz" {
		t.Fatalf("sanitized input = %q", got)
	}
	if len([]rune(got)) != 8 {
		t.Fatalf("sanitized input length = %d, want 8", len([]rune(got)))
	}
}
