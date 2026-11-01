package core

import (
	"context"
	"os"
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func addReferenceOutput(t *testing.T, b *Bot, call string, incomplete bool) string {
	t.Helper()
	store := b.ag.Executor().ExecutionState().OutputStore
	path, err := store.PreserveOutput("original output " + call)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.toolbox.Workspace().AddSessionRoot(path, workspace.AccessReadOnly); err != nil {
		t.Fatal(err)
	}
	b.ctxMgr.AddAssistant(types.Message{ToolCalls: []types.ToolCall{{ID: call, Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}})
	b.ctxMgr.AddToolResultsBatch([]ctxmgr.ToolResultMsg{{ToolName: "shell", Message: types.Message{ToolCallID: call, Content: "preview", ToolOutput: protocol.ToolOutputArtifact{Path: path, Incomplete: incomplete}}}})
	return path
}

func referenceActorContext(t *testing.T, b *Bot) context.Context {
	t.Helper()
	return workspace.WithManager(toolcore.WithOutputStore(t.Context(), b.ag.Executor().ExecutionState().OutputStore), b.toolbox.Workspace())
}

func TestContextReferencesSurvivePendingDurableAndRestore(t *testing.T) {
	b := newPersistTestBot(t)
	path := addReferenceOutput(t, b, "saved", true)
	scope := b.CurrentSessionID()
	q := refbridge.Query{Limit: 1}
	first, err := b.contextReferences(t.Context(), scope, q)
	if err != nil || len(first.Items) != 1 || first.Items[0].Sequence != 2 || !first.Items[0].Incomplete || first.Scanned != 1 {
		t.Fatalf("pending page=%+v err=%v", first, err)
	}
	id := first.Items[0].ID
	for range 2 {
		if err := b.saveSession(); err != nil {
			t.Fatal(err)
		}
		if err := b.ResumeSession(scope); err != nil {
			t.Fatal(err)
		}
		// Replay-only assistant headers must not shift source sequence numbers.
		page, err := b.contextReferences(t.Context(), scope, q)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != id || page.Scanned != 1 {
			t.Fatalf("restored page=%+v err=%v", page, err)
		}
	}
	inspected, err := b.contextReferences(referenceActorContext(t, b), scope, refbridge.Query{ID: id, Limit: 1})
	if err != nil || inspected.Status != "available" || inspected.Path != path {
		t.Fatalf("inspect=%+v err=%v", inspected, err)
	}
	for range 25 {
		b.ctxMgr.Add("user", "later question")
	}
	recent := b.modelContextReferences(t.Context(), scope)
	if recent.HasMore == false || len(recent.Items) != 0 {
		t.Fatalf("window pretending to be inventory: %+v", recent)
	}
	older, err := b.contextReferences(t.Context(), scope, refbridge.Query{Before: recent.Before, Limit: 20})
	if err != nil || len(older.Items) != 1 || older.Items[0].ID != id {
		t.Fatalf("old reference lost: %+v %v", older, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	inspected, err = b.contextReferences(referenceActorContext(t, b), scope, refbridge.Query{ID: id, Limit: 1})
	if err != nil || inspected.Status != "missing" || inspected.Path != "" {
		t.Fatalf("deleted artifact=%+v %v", inspected, err)
	}
}

func TestContextReferenceInspectionChecksScopeActorAndWorkspace(t *testing.T) {
	b := newPersistTestBot(t)
	path := addReferenceOutput(t, b, "private", false)
	scope := b.CurrentSessionID()
	page, err := b.contextReferences(t.Context(), scope, refbridge.Query{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	id := page.Items[0].ID
	if strings.Contains(id, path) {
		t.Fatal("ID contains raw path")
	}
	unavailable, err := b.contextReferences(t.Context(), scope, refbridge.Query{ID: id, Limit: 1})
	if err != nil || unavailable.Status != "unavailable" || unavailable.Path != "" {
		t.Fatal("inspection bypassed actor binding")
	}
	deniedWorkspace := workspace.New(t.TempDir(), nil)
	ctx := workspace.WithManager(toolcore.WithOutputStore(t.Context(), b.ag.Executor().ExecutionState().OutputStore), deniedWorkspace)
	unavailable, err = b.contextReferences(ctx, scope, refbridge.Query{ID: id, Limit: 1})
	if err != nil || unavailable.Status != "unavailable" || unavailable.Path != "" {
		t.Fatal("inspection bypassed directory authority")
	}
	child := protocol.WithExecutionIdentity(referenceActorContext(t, b), protocol.ExecutionIdentity{TaskID: "child"})
	if _, err := b.contextReferences(child, scope, refbridge.Query{Limit: 20}); err == nil {
		t.Fatal("child saw main actor references")
	}
	wrongStore := toolcore.WithOutputStore(referenceActorContext(t, b), toolcore.NewOutputStore(t.TempDir()))
	unavailable, err = b.contextReferences(wrongStore, scope, refbridge.Query{ID: id, Limit: 1})
	if err == nil || unavailable.Path != "" {
		t.Fatal("inspection bypassed output actor ownership")
	}
	if _, err := b.contextReferences(wrongStore, scope, refbridge.Query{Limit: 20}); err == nil {
		t.Fatal("actor with no TaskID saw main references")
	}
	if _, err := b.readContextReferences(t.Context(), refbridge.Query{Limit: 20}); err == nil {
		t.Fatal("unbound tool saw main references")
	}
	results := b.ag.Executor().ExecuteBatch(t.Context(), []toolcore.ToolCallItem{{ID: "refs", Name: "context_refs", Args: map[string]any{"action": "inspect", "id": id}}})
	if len(results) != 1 || results[0].Error != "" || !strings.Contains(results[0].Output, `"status":"available"`) {
		t.Fatalf("executor reference lookup failed: %+v", results)
	}
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.contextReferences(t.Context(), scope, refbridge.Query{Limit: 20}); err == nil {
		t.Fatal("old scope accepted after switch")
	}
	addReferenceOutput(t, b, "other", false)
	unavailable, err = b.contextReferences(referenceActorContext(t, b), b.CurrentSessionID(), refbridge.Query{ID: id, Limit: 1})
	if err != nil || unavailable.Status != "changed" || unavailable.Path != "" {
		t.Fatal("cross-session reference resolved")
	}
}

func TestContextReferenceCursorExcludesNewerAppends(t *testing.T) {
	b := newPersistTestBot(t)
	addReferenceOutput(t, b, "one", false)
	addReferenceOutput(t, b, "two", false)
	scope := b.CurrentSessionID()
	first, err := b.contextReferences(t.Context(), scope, refbridge.Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	addReferenceOutput(t, b, "three", false)
	next, err := b.contextReferences(t.Context(), scope, refbridge.Query{Before: first.Before, Limit: 2})
	if err != nil || len(first.Items) != 1 || len(next.Items) != 1 || next.Items[0].Sequence != 2 || first.Items[0].Sequence != 4 {
		t.Fatalf("cursor shifted: %+v %+v %v", first, next, err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := b.contextReferences(canceled, scope, refbridge.Query{Limit: 20}); err == nil {
		t.Fatal("ignored cancellation")
	}
	for _, id := range []string{"/tmp/result.txt", "artifact.01." + strings.Repeat("a", 64), "artifact.-1." + strings.Repeat("a", 64)} {
		if _, err := referenceSequence(id); err == nil {
			t.Fatal("accepted malformed ID")
		}
	}
}
