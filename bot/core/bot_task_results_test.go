package core

import (
	"context"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
	"nekocode/protocol"
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

func TestAlreadyRenderedForkSourceIsAuthorizedButNotInjectedTwice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := session.OpenSubagent("parent", "source", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("executor", "produce", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "decisive-source-evidence"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	page, err := session.ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: "source", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	captured, read, text, err := captureResultInputs(t.Context(), "parent", []string{page.Ref}, page.Ref)
	if err != nil || len(captured) != 1 || text != "" || read == nil {
		t.Fatalf("source duplicated or lost: %q %v", text, err)
	}
	if _, err := read(t.Context(), taskbridge.ResultQuery{Ref: page.Ref, Limit: 100}); err != nil {
		t.Fatal("deduplication removed explicit source access", err)
	}
	if _, _, _, err := captureResultInputs(t.Context(), "foreign-parent", []string{page.Ref}, page.Ref); err == nil {
		t.Fatal("rendered source bypassed parent validation")
	}
}

type missingRuntimeResultReader struct{}

func (missingRuntimeResultReader) ReadBackgroundResult(context.Context, string, protocol.ResultQuery) (protocol.ResultPage, error) {
	return protocol.ResultPage{}, protocol.ErrBackgroundResultNotFound
}

func TestRuntimeOwnedAttemptCannotFallBackAfterReceiptEviction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := session.OpenSubagent("parent", "worker", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := child.BeginBackground("runtime-attempt", "executor", "produce", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "completed child transcript before runtime commit failed"); err != nil {
		t.Fatal(err)
	}
	page, err := session.ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{ID: "worker", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	ctx := protocol.WithBackgroundResultReader(t.Context(), missingRuntimeResultReader{})
	for _, q := range []taskbridge.ResultQuery{{ID: "worker", Limit: 20}, {ID: "worker", AttemptID: page.AttemptID, Limit: 20}, {Ref: page.Ref, Limit: 20}} {
		if _, err := readTaskResult(ctx, "parent", q); err == nil || !strings.Contains(err.Error(), "owned by runtime") {
			t.Fatalf("missing canonical report replaced with child success: %v", err)
		}
	}
}
