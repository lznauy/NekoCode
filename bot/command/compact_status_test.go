package command

import (
	ctxmgr "nekocode/bot/contextmgr"
	"strings"
	"testing"
)

func TestForcedCompactDoesNotMisreportUnavailableSummary(t *testing.T) {
	m := ctxmgr.New(ctxmgr.Config{})
	m.Add("user", "task")
	m.Add("assistant", "progress")
	m.Add("assistant", "more progress")
	result, err := ForceCompact(t.Context(), m, true)
	if err != nil || strings.Contains(result, "Not needed") || !strings.Contains(result, "Unable to compact") {
		t.Fatalf("result=%q err=%v", result, err)
	}
	result, err = ForceCompact(t.Context(), m, false)
	if err != nil || !strings.Contains(result, "Not needed") {
		t.Fatalf("below-threshold automatic result=%q err=%v", result, err)
	}
}
