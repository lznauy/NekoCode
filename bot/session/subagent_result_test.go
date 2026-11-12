package session

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSubagentResultPagesAreCompleteUnicodeAndPinnedAcrossResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "report", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("executor", "review", nil); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("审查证据🙂\n", 1200) + "REPORT_TAIL"
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", body); err != nil {
		t.Fatal(err)
	}
	oldID := child.Snapshot().Subagent.CurrentAttemptID
	child.Close()
	first, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: "report", Limit: 97})
	if err != nil {
		t.Fatal(err)
	}
	assembled := first.Content
	page := first
	for page.HasMore {
		page, err = ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{Ref: first.Ref, Offset: page.NextOffset, Limit: 97})
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(page.Content) || page.Ref != first.Ref {
			t.Fatal("page split UTF-8 or changed version")
		}
		assembled += page.Content
	}
	if assembled != body || first.AttemptID != oldID {
		t.Fatal("complete report was truncated or wrong attempt")
	}
	resumed, err := OpenSubagent("parent", "report", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Begin("executor", "recheck", nil); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Checkpoint(ctxmgr.ManagerSnapshot{}, "running", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: "report", Limit: 20}); err == nil {
		t.Fatal("running result exposed as final")
	}
	if err := resumed.Checkpoint(ctxmgr.ManagerSnapshot{}, "partial", "NEW_RESULT"); err != nil {
		t.Fatal(err)
	}
	latest, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: "report", Limit: 20})
	if err != nil || latest.AttemptID == oldID || latest.Status != "partial" {
		t.Fatalf("wrong new result: %+v %v", latest, err)
	}
	old, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{Ref: first.Ref, Limit: 97})
	if err != nil || old.Content != first.Content {
		t.Fatal("resume changed pinned old result")
	}
	if _, err := ReadSubagentResult(t.Context(), "other", taskbridge.ResultQuery{Ref: first.Ref, Limit: 20}); err == nil {
		t.Fatal("cross-parent reference accepted")
	}
	if _, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: "../report", Limit: 20}); err == nil {
		t.Fatal("path accepted as ID")
	}
	if _, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: "report", Offset: 1, Limit: 20}); err == nil {
		t.Fatal("unpinned continuation accepted")
	}
	for i := range resumed.snapshot.Subagent.Attempts {
		if resumed.snapshot.Subagent.Attempts[i].ID == oldID {
			resumed.snapshot.Subagent.Attempts[i].Result = "tampered result"
		}
	}
	if err := resumed.snapshot.saveAt(resumed.root); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{Ref: first.Ref, Limit: 20}); err == nil {
		t.Fatal("changed result silently reused old reference")
	}
	resumed.Close()
}

func TestResultInputScopePersistsAndCannotExpand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "consumer", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetResultInputs([]string{"ref-a", "ref-b"}); err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("executor", "consume", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	snapshot := child.Snapshot()
	snapshot.Subagent.ResultInputs[0] = "mutated"
	if child.Snapshot().Subagent.ResultInputs[0] != "ref-a" {
		t.Fatal("input scope leaked via snapshot")
	}
	child.Close()
	meta, err := ReadSubagentMetadata("parent", "consumer")
	if err != nil || len(meta.ResultInputs) != 2 || meta.ResultInputs[0] != "ref-a" {
		t.Fatalf("canonical inputs missing: %+v %v", meta, err)
	}
	resumed, err := OpenSubagent("parent", "consumer", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if err := resumed.SetResultInputs([]string{"ref-a", "ref-c"}); err == nil {
		t.Fatal("input scope expanded")
	}
	if err := resumed.SetResultInputs([]string{"ref-a"}); err != nil {
		t.Fatal(err)
	}
}

func TestSubagentResultIdentifiesWorkspaceIsolation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, isolation := range []string{"shared", "worktree"} {
		child, err := OpenSubagent("parent", isolation, "/project", false)
		if err != nil {
			t.Fatal(err)
		}
		if err := child.SetIsolation(isolation); err != nil {
			t.Fatal(err)
		}
		if err := child.Begin("executor", "produce", nil); err != nil {
			t.Fatal(err)
		}
		if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "source report"); err != nil {
			t.Fatal(err)
		}
		child.Close()
		page, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: isolation, Limit: 3})
		if err != nil || page.Isolation != isolation {
			t.Fatalf("workspace provenance missing: %+v %v", page, err)
		}
		page, err = ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{Ref: page.Ref, Offset: page.NextOffset, Limit: 3})
		if err != nil || page.Isolation != isolation {
			t.Fatalf("next page lost workspace provenance: %+v %v", page, err)
		}
	}
}

func TestSubagentWorkspaceKindSurvivesRestartAndResultPaging(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, kind := range []string{"shared", "head", "snapshot"} {
		child, err := OpenSubagent("parent", kind, "/project", false)
		if err != nil {
			t.Fatal(err)
		}
		isolation := "worktree"
		if kind == "shared" {
			isolation = "shared"
		}
		if err = child.SetIsolation(isolation); err != nil {
			t.Fatal(err)
		}
		if err = child.SetWorkspaceKind(kind); err != nil {
			t.Fatal(err)
		}
		if err = child.Begin("executor", "inspect", nil); err != nil {
			t.Fatal(err)
		}
		if err = child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "report"); err != nil {
			t.Fatal(err)
		}
		child.Close()
		page, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: kind, Limit: 20})
		if err != nil || page.WorkspaceKind != kind || page.Isolation != isolation {
			t.Fatalf("workspace identity lost: %+v %v", page, err)
		}
		infos, err := ListSubagents("parent")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, info := range infos {
			if info.ID == kind && info.WorkspaceKind == kind {
				found = true
			}
		}
		if !found {
			t.Fatal("archive lost workspace kind")
		}
		resumed, err := OpenSubagent("parent", kind, "/project", true)
		if err != nil {
			t.Fatal(err)
		}
		if kind == "snapshot" && resumed.SetWorkspaceKind("head") == nil {
			t.Fatal("resume changed workspace construction")
		}
		resumed.Close()
	}
}

func TestWorkspaceBaselinePersistsAndIsExposedOnEveryResultPage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, kind := range []string{"head", "snapshot"} {
		child, err := OpenSubagent("parent", kind, "/project", false)
		if err != nil {
			t.Fatal(err)
		}
		if err = child.SetIsolation("worktree"); err != nil {
			t.Fatal(err)
		}
		if err = child.SetWorkspaceKind(kind); err != nil {
			t.Fatal(err)
		}
		base, head := strings.Repeat("a", 40), ""
		if kind == "snapshot" {
			head = strings.Repeat("b", 40)
		}
		if err = child.SetWorkspaceBaseline(base, head); err != nil {
			t.Fatal(err)
		}
		child.Close()
		child, err = OpenSubagent("parent", kind, "/project", true)
		if err != nil {
			t.Fatal(err)
		}
		if child.Snapshot().Subagent.WorkspaceBase != base {
			t.Fatal("baseline lost before first checkpoint")
		}
		if err = child.SetWorkspaceBaseline(strings.Repeat("c", 40), head); err == nil {
			t.Fatal("resume changed baseline")
		}
		if err = child.SetWorkspaceBaseline("HEAD", head); err == nil {
			t.Fatal("accepted mutable ref")
		}
		if err = child.Begin("executor", "review", nil); err != nil {
			t.Fatal(err)
		}
		if err = child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "long report"); err != nil {
			t.Fatal(err)
		}
		child.Close()
		first, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: kind, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		next, err := ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{Ref: first.Ref, Offset: first.NextOffset, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range []taskbridge.ResultPage{first, next} {
			if page.WorkspaceBase != base || page.SourceHead != head {
				t.Fatalf("lost content identity: %+v", page)
			}
		}
	}
}
