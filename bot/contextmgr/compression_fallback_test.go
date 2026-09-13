package contextmgr

import (
	"testing"

	"nekocode/bot/provider/types"
)

func TestExtractXMLBlock(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"wrapped", "noise before <summary>the summary</summary> noise after", "the summary"},
		{"missing open tag", "just plain text", ""},
		{"unclosed block", "intro <summary>partial content", "partial content"},
	}
	for _, c := range cases {
		if got := extractXMLBlock(c.raw, "summary"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSummarizeFallsBackToRawWithoutSummaryTags(t *testing.T) {
	raw := "plain summary without any XML tags, but long enough to serve as a usable archive"
	rc := newReplacementCompactor(func([]types.Message, string) (string, error) {
		return raw, nil
	}, 80)
	history := []types.Message{
		{Role: "user", Content: "turn one"},
		{Role: "assistant", Content: "reply one"},
		{Role: "user", Content: "turn two"},
		{Role: "assistant", Content: "reply two"},
		{Role: "user", Content: "turn three"},
		{Role: "assistant", Content: "reply three"},
		{Role: "user", Content: "turn four"},
	}
	archive, recent, trimmed, err := rc.summarize(history, "", 1_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if archive != raw {
		t.Fatalf("archive should fall back to the raw summarizer output, got %q", archive)
	}
	if len(recent) >= len(history) || trimmed == 0 {
		t.Fatalf("expected recent window to shrink history, got recent=%d trimmed=%d", len(recent), trimmed)
	}
}

func TestSummarizePlaceholderWhenOutputTooSmall(t *testing.T) {
	rc := newReplacementCompactor(func([]types.Message, string) (string, error) {
		return "tiny", nil
	}, 80)
	history := []types.Message{
		{Role: "user", Content: "turn one"},
		{Role: "assistant", Content: "reply one"},
		{Role: "user", Content: "turn two"},
		{Role: "assistant", Content: "reply two"},
		{Role: "user", Content: "turn three"},
		{Role: "assistant", Content: "reply three"},
		{Role: "user", Content: "turn four"},
	}
	archive, _, _, err := rc.summarize(history, "", 1_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if archive != archiveUnavailable {
		t.Fatalf("tiny output should keep the placeholder, got %q", archive)
	}
}

// A short <summary> block is still a summary: the size floor only guards a
// reply that ignored the format, and it counts runes so CJK is not penalized.
func TestSummarizeKeepsShortWrappedSummary(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "turn one"},
		{Role: "assistant", Content: "reply one"},
		{Role: "user", Content: "turn two"},
		{Role: "assistant", Content: "reply two"},
		{Role: "user", Content: "turn three"},
		{Role: "assistant", Content: "reply three"},
		{Role: "user", Content: "turn four"},
	}
	for _, c := range []struct{ raw, want string }{
		{"<summary>完成</summary>", "完成"},                                               // very short CJK
		{"<summary>done: login; 2 tests left</summary>", "done: login; 2 tests left"}, // short ASCII
	} {
		rc := newReplacementCompactor(func([]types.Message, string) (string, error) { return c.raw, nil }, 80)
		archive, _, trimmed, err := rc.summarize(history, "", 1_000_000, nil)
		if err != nil {
			t.Fatal(err)
		}
		if archive != c.want {
			t.Fatalf("wrapped summary %q was discarded for %q", c.raw, archive)
		}
		if trimmed == 0 {
			t.Fatalf("wrapped summary %q did not compact", c.raw)
		}
	}
}

// An empty wrapper is not a summary either: storing the bare tags as an archive
// would look like one while carrying no content.
func TestSummarizeRejectsEmptyWrappedSummary(t *testing.T) {
	rc := newReplacementCompactor(func([]types.Message, string) (string, error) {
		return "<summary></summary>", nil
	}, 80)
	history := []types.Message{
		{Role: "user", Content: "turn one"},
		{Role: "assistant", Content: "reply one"},
		{Role: "user", Content: "turn two"},
		{Role: "assistant", Content: "reply two"},
		{Role: "user", Content: "turn three"},
		{Role: "assistant", Content: "reply three"},
		{Role: "user", Content: "turn four"},
	}
	archive, _, _, err := rc.summarize(history, "", 1_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if archive != archiveUnavailable {
		t.Fatalf("empty wrapper should keep the placeholder, got %q", archive)
	}
}

// An unwrapped reply shorter than the floor is a refusal, not a summary.
func TestSummarizeFallsBackForShortUnwrappedReply(t *testing.T) {
	rc := newReplacementCompactor(func([]types.Message, string) (string, error) {
		return "我无法总结", nil // 5 runes, no tags
	}, 80)
	history := []types.Message{
		{Role: "user", Content: "turn one"},
		{Role: "assistant", Content: "reply one"},
		{Role: "user", Content: "turn two"},
		{Role: "assistant", Content: "reply two"},
		{Role: "user", Content: "turn three"},
		{Role: "assistant", Content: "reply three"},
		{Role: "user", Content: "turn four"},
	}
	archive, _, _, err := rc.summarize(history, "", 1_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if archive != archiveUnavailable {
		t.Fatalf("short unwrapped reply should keep the placeholder, got %q", archive)
	}
}
