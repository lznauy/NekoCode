package contextmgr

import (
	"nekocode/bot/provider/types"
	"testing"
)

func TestExtractXMLBlock(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"noise <summary>the summary</summary> noise", "the summary"},
		{"just plain text", ""},
		{"intro <summary>partial content", ""},
	} {
		if got := extractXMLBlock(tc.raw, "summary"); got != tc.want {
			t.Fatalf("extract %q = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSummaryRejectsMalformedOutputWithoutDroppingHistory(t *testing.T) {
	for _, raw := range []string{"", "tiny", "a long unwrapped response that could also be a refusal", "<summary></summary>", "<summary>incomplete"} {
		t.Run(raw, func(t *testing.T) {
			rc := newReplacementCompactor(func([]types.Message, string) (string, error) { return raw, nil }, 80)
			history := summaryTestHistory("old request")
			archive, recent, trimmed, err := rc.summarize(history, "old archive", 64000, nil)
			if err == nil || archive != "" || recent != nil || trimmed != 0 {
				t.Fatalf("invalid result accepted: archive=%q trimmed=%d err=%v", archive, trimmed, err)
			}
			if history[0].Content != "old request" {
				t.Fatal("source history mutated")
			}
		})
	}
}

func TestSummarizeKeepsShortWrappedSummary(t *testing.T) {
	rc := newReplacementCompactor(func([]types.Message, string) (string, error) { return "<summary>完成</summary>", nil }, 80)
	archive, _, trimmed, err := rc.summarize(summaryTestHistory("old request"), "", 64000, nil)
	if err != nil || archive != "完成" || trimmed == 0 {
		t.Fatalf("short valid summary = %q, %d, %v", archive, trimmed, err)
	}
}
