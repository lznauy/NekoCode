package session

import (
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/protocol"
)

func TestAttemptSummarySurvivesReloadWithoutLeakingIntoResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := child.Begin("explore", "inspect", nil); err != nil {
		t.Fatal(err)
	}
	want := protocol.RunSummary{StepCount: 4, StopReason: "step_limit"}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "partial", "unfinished", SubagentAttemptUsage{Summary: want}); err != nil {
		t.Fatal(err)
	}
	page, err := SubagentAttempts("parent", "child", 0, 10)
	if err != nil || len(page.Attempts) != 1 || page.Attempts[0].Summary != want {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if err := child.Begin("explore", "continue", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "running", ""); err != nil {
		t.Fatal(err)
	}
	page, err = SubagentAttempts("parent", "child", 0, 10)
	if err != nil || len(page.Attempts) != 2 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if page.Attempts[0].Summary != want || page.Attempts[1].Summary != (protocol.RunSummary{}) {
		t.Fatal("summary leaked across attempts")
	}
}
