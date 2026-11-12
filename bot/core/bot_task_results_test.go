package core

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
	"strings"
	"testing"
)

func TestSharedResultReaderOnlyAllowsExplicitPinnedInputs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	refs := []string{}
	for _, id := range []string{"a", "b"} {
		child, err := session.OpenSubagent("parent", id, "/project", false)
		if err != nil {
			t.Fatal(err)
		}
		if err := child.Begin("executor", "produce", nil); err != nil {
			t.Fatal(err)
		}
		if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "result "+id); err != nil {
			t.Fatal(err)
		}
		child.Close()
		page, err := session.ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: id, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, page.Ref)
	}
	captured, reader, prompt, err := captureResultInputs(t.Context(), "parent", refs[:1])
	if err != nil {
		t.Fatal(err)
	}
	if len(captured) != 1 || !strings.Contains(prompt, refs[0]) || strings.Contains(prompt, refs[1]) {
		t.Fatal("wrong shared evidence context")
	}
	if _, err := reader(t.Context(), taskbridge.ResultQuery{Ref: refs[0], Limit: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader(t.Context(), taskbridge.ResultQuery{Ref: refs[1], Limit: 20}); err == nil {
		t.Fatal("unshared sibling result exposed")
	}
	if _, err := reader(t.Context(), taskbridge.ResultQuery{ID: "a", Limit: 20}); err == nil {
		t.Fatal("unpinned sibling lookup exposed")
	}
	if _, _, _, err := captureResultInputs(t.Context(), "other", refs[:1]); err == nil {
		t.Fatal("foreign-parent result shared")
	}
	if _, _, _, err := captureResultInputs(t.Context(), "parent", []string{refs[0], refs[0]}); err == nil {
		t.Fatal("duplicate inputs accepted")
	}
}
