package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/contextrefs"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
	"nekocode/protocol"
)

func TestReferenceReaderRejectsOtherActorAttemptAndStore(t *testing.T) {
	store := core.NewOutputStore(t.TempDir())
	mgr := ctxmgr.New(ctxmgr.Config{})
	mgr.RecordSkillWorkflows([]string{"review"}, []string{"workflow"})
	cfg := RunConfig{SessionID: "parent", SubagentID: "child", AttemptID: "attempt", Profile: Profile{Tools: []string{"context_refs"}}, References: refbridge.Source{Skill: func(string) (string, bool) { return "workflow", true }}}
	read := referenceReader(cfg, mgr, store)
	ctx := protocol.WithExecutionIdentity(core.WithOutputStore(t.Context(), store), protocol.ExecutionIdentity{TaskID: cfg.SubagentID, AttemptID: cfg.AttemptID})
	page, err := read(ctx, refbridge.Query{Limit: 20})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	id := page.Items[0].ID
	inspected, err := read(ctx, refbridge.Query{Limit: 1, ID: id})
	if err != nil || inspected.Status != "available" {
		t.Fatalf("inspect=%+v err=%v", inspected, err)
	}
	for _, bad := range []context.Context{
		t.Context(),
		protocol.WithExecutionIdentity(ctx, protocol.ExecutionIdentity{TaskID: "sibling", AttemptID: cfg.AttemptID}),
		protocol.WithExecutionIdentity(ctx, protocol.ExecutionIdentity{TaskID: cfg.SubagentID, AttemptID: "older"}),
		core.WithOutputStore(ctx, core.NewOutputStore(t.TempDir())),
	} {
		if _, err := read(bad, refbridge.Query{Limit: 20}); err == nil {
			t.Fatal("foreign actor accepted")
		}
	}
	other := cfg
	other.SessionID = "other-parent"
	foreign, err := referenceReader(other, mgr, store)(ctx, refbridge.Query{Limit: 20})
	if err != nil || foreign.Items[0].ID == id {
		t.Fatal("parent scope not included in ID")
	}
	changed, err := read(ctx, refbridge.Query{Limit: 1, ID: foreign.Items[0].ID})
	if err != nil || changed.Status != "changed" {
		t.Fatalf("foreign reference resolved: %+v %v", changed, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := read(canceled, refbridge.Query{Limit: 20}); err == nil {
		t.Fatal("cancellation ignored")
	}
	cfg.Profile.Tools = nil
	if referenceReader(cfg, mgr, store) != nil {
		t.Fatal("profile without tool gained queries")
	}
}

func TestConcurrentSubagentsUseLocalReadersWithoutRewiringTools(t *testing.T) {
	registry := tools.New()
	registry.SetWorkspace(workspace.New(t.TempDir(), nil))
	tool := contextrefs.New()
	var parentCalls atomic.Int32
	tool.Wire(func(context.Context, refbridge.Query) (refbridge.Page, error) {
		parentCalls.Add(1)
		return refbridge.Page{}, fmt.Errorf("parent reader")
	})
	registry.Register(tool)
	parent := execution.NewExecutionState()
	parent.OutputStore = core.NewOutputStore(t.TempDir())
	var wg sync.WaitGroup
	for _, name := range []string{"one", "two"} {
		name := name
		path, err := parent.OutputStore.Child("subagents").Child(name).PreserveOutput("private " + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Workspace().AddSessionRoot(path, workspace.AccessReadOnly); err != nil {
			t.Fatal(err)
		}
		msg := types.Message{Role: "tool", ToolCallID: "old", ToolOutput: protocol.ToolOutputArtifact{Path: path}}
		cfg := RunConfig{SessionID: "parent", SubagentID: name, AttemptID: "attempt-" + name, ToolState: parent, Profile: Profile{Name: "explore", Tools: []string{"context_refs"}}, ResumeContext: &ctxmgr.ManagerSnapshot{Transcript: []types.Message{msg}}}
		id := refbridge.ArtifactReference(referenceScope(cfg), 1, msg.ToolOutput).ID
		llm := &scriptedLLM{scripts: [][]types.StreamToken{
			{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "list", Name: "context_refs", Arguments: `{"action":"list"}`}}},
			{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "inspect", Name: "context_refs", Arguments: fmt.Sprintf(`{"action":"inspect","id":%q}`, id)}}},
			completionScript(),
		}}
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := New(Config{LLM: llm, Tools: registry}).Run(t.Context(), cfg)
			if err != nil || result.Status != StatusCompleted {
				t.Errorf("%s: %+v %v", name, result, err)
				return
			}
			if !messagesContainText(llm.messages[0], id) {
				t.Errorf("%s missing local projection", name)
			}
			seen := map[string]refbridge.Page{}
			for _, m := range llm.messages[2] {
				if m.Role == "tool" && (m.ToolCallID == "list" || m.ToolCallID == "inspect") {
					var p refbridge.Page
					if err := json.Unmarshal([]byte(m.Content), &p); err != nil {
						t.Error(err)
					}
					seen[m.ToolCallID] = p
				}
			}
			if len(seen["list"].Items) != 1 || seen["list"].Items[0].ID != id || seen["inspect"].Path != path || seen["inspect"].Status != "available" {
				t.Errorf("%s wrong scope: %+v", name, seen)
			}
		}()
	}
	wg.Wait()
	if parentCalls.Load() != 0 {
		t.Fatal("child fell back to shared parent reader")
	}
	// The parent binding remains unchanged after both child runs.
	if _, err := tool.Execute(t.Context(), map[string]any{"action": "list"}); err == nil || parentCalls.Load() != 1 {
		t.Fatal("shared tool binding mutated")
	}
}

func TestSubagentReferencesSurviveColdResumeAndCompactedHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := session.OpenSubagent("parent", "child", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "first", nil); err != nil {
		t.Fatal(err)
	}
	history := []types.Message{{Role: "user", Source: types.MessageSourceRuntimeEvent, SkillDelivery: types.SkillDelivery{Schema: 1, Name: "review", Source: "delegated", State: types.SkillReady, ContentHash: types.SkillContentHash("workflow"), OriginSequence: 1}}}
	for range 1000 {
		history = append(history, types.Message{Role: "user", Content: "ordinary history"})
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{Messages: history[len(history)-1:], Transcript: history}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	child, err = session.OpenSubagent("parent", "child", "/workspace", true)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	restored := child.Snapshot().ContextSnapshot()
	if len(restored.Transcript) != 0 || restored.TranscriptBase != len(history) {
		t.Fatal("resume retained full transcript")
	}
	if err := child.Begin("explore", "resume", nil); err != nil {
		t.Fatal(err)
	}
	cfg := RunConfig{SessionID: "parent", SubagentID: "child", AttemptID: child.Snapshot().Subagent.CurrentAttempt().ID, Prompt: "resume", Profile: Profile{Name: "explore", Tools: []string{"context_refs"}}, ResumeContext: &restored, References: child.ReferenceSource()}
	cfg.References.Skill = func(string) (string, bool) { return "workflow", true }
	cfg.SaveContext = func(s ctxmgr.ManagerSnapshot, status, output string, _ AttemptUsage) error {
		return child.Checkpoint(s, status, output)
	}
	id, ok := refbridge.SkillReference(referenceScope(cfg), 1, history[0])
	if !ok {
		t.Fatal("bad skill fixture")
	}
	llm := &scriptedLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "inspect", Name: "context_refs", Arguments: fmt.Sprintf(`{"action":"inspect","id":%q}`, id.ID)}}}, completionScript(),
	}}
	registry := tools.New()
	registry.Register(contextrefs.New())
	result, err := New(Config{LLM: llm, Tools: registry}).Run(t.Context(), cfg)
	if err != nil || result.Status != StatusCompleted {
		t.Fatalf("%+v %v", result, err)
	}
	if !messagesContainText(llm.messages[0], id.ID) {
		t.Fatal("cold projection lost old reference")
	}
	found := false
	for _, m := range llm.messages[1] {
		if m.ToolCallID == "inspect" {
			found = strings.Contains(m.Content, `"status":"available"`)
		}
	}
	if !found {
		t.Fatal("cold inspection did not resolve durable record")
	}
}

func TestUnboundOrDisabledChildNeverInheritsParentReferences(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint(disabled), func(t *testing.T) {
			registry := tools.New()
			tool := contextrefs.New()
			parentCalls := 0
			parentReader := func(context.Context, refbridge.Query) (refbridge.Page, error) {
				parentCalls++
				return refbridge.Page{SessionID: "parent"}, nil
			}
			tool.Wire(parentReader)
			registry.Register(tool)
			cfg := RunConfig{SessionID: "parent", SubagentID: "child", Profile: Profile{Name: "custom", Tools: []string{"context_refs"}}}
			if disabled {
				cfg.AttemptID = "attempt"
				cfg.Profile.Tools = nil
			}
			llm := &scriptedLLM{scripts: [][]types.StreamToken{
				{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "query", Name: "context_refs", Arguments: `{"action":"list"}`}}}, completionScript(),
			}}
			result, err := New(Config{LLM: llm, Tools: registry}).Run(refbridge.WithReader(t.Context(), parentReader), cfg)
			if err != nil || result.Status != StatusCompleted {
				t.Fatalf("%+v %v", result, err)
			}
			if parentCalls != 0 {
				t.Fatal("child inherited parent query binding")
			}
			if messagesContainText(llm.messages[0], `"kind":"context_references"`) {
				t.Fatal("unbound child received reference projection")
			}
			blocked := false
			for _, m := range llm.messages[1] {
				if m.ToolCallID == "query" {
					blocked = m.IsError
				}
			}
			if !blocked {
				t.Fatal("unbound or disabled query succeeded")
			}
		})
	}
}

func TestChildReferenceInspectionDoesNotAuthorizeForeignOutputs(t *testing.T) {
	parent := core.NewOutputStore(t.TempDir())
	own := parent.Child("subagents").Child("child")
	foreignPath, err := parent.Child("subagents").Child("other").PreserveOutput("private sibling")
	if err != nil {
		t.Fatal(err)
	}
	mgr := ctxmgr.New(ctxmgr.Config{})
	artifact := protocol.ToolOutputArtifact{Path: foreignPath}
	mgr.Restore(ctxmgr.ManagerSnapshot{Transcript: []types.Message{{Role: "tool", ToolOutput: artifact}}})
	cfg := RunConfig{SessionID: "parent", SubagentID: "child", AttemptID: "attempt", Profile: Profile{Tools: []string{"context_refs"}}}
	// Even broad workspace access does not make another store's file owned.
	ctx := workspace.WithManager(core.WithOutputStore(t.Context(), own), workspace.New(t.TempDir(), nil))
	w, _ := workspace.FromContext(ctx)
	if _, err := w.AddSessionRoot(foreignPath, workspace.AccessReadOnly); err != nil {
		t.Fatal(err)
	}
	ctx = protocol.WithExecutionIdentity(ctx, protocol.ExecutionIdentity{TaskID: cfg.SubagentID, AttemptID: cfg.AttemptID})
	id := refbridge.ArtifactReference(referenceScope(cfg), 1, artifact).ID
	page, err := referenceReader(cfg, mgr, own)(ctx, refbridge.Query{Limit: 1, ID: id})
	if err != nil || page.Status != "unavailable" || page.Path != "" {
		t.Fatalf("foreign output resolved: %+v %v", page, err)
	}
}

func TestEvidenceOnlySubagentsUseOwnConcurrentReadersWithoutContextProjection(t *testing.T) {
	registry := tools.New()
	query := contextrefs.NewEvidence()
	var parentCalls atomic.Int32
	query.Wire(func(context.Context, refbridge.Query) (refbridge.Page, error) {
		parentCalls.Add(1)
		return refbridge.Page{}, fmt.Errorf("parent reader")
	})
	registry.Register(query)
	var wg sync.WaitGroup
	for _, name := range []string{"first", "second"} {
		name := name
		receipt := protocol.ExecutionReceipt{ID: uuid.NewString(), Tool: "read", Status: "success", Execution: protocol.ExecutionIdentity{TaskID: name, AttemptID: "old-" + name}}
		msg := types.Message{Role: "tool", ToolEvidence: protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{receipt}}}
		cfg := RunConfig{SessionID: "parent", SubagentID: name, AttemptID: "current-" + name, Profile: Profile{Name: "custom", Tools: []string{"evidence"}}, ResumeContext: &ctxmgr.ManagerSnapshot{Transcript: []types.Message{msg}}}
		model := &scriptedLLM{scripts: [][]types.StreamToken{
			{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "list", Name: "evidence", Arguments: `{"action":"list"}`}}}, completionScript(),
		}}
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := New(Config{LLM: model, Tools: registry}).Run(t.Context(), cfg)
			if err != nil || result.Status != StatusCompleted {
				t.Errorf("%+v %v", result, err)
				return
			}
			if messagesContainText(model.messages[0], `"kind":"context_references"`) {
				t.Error("evidence-only profile gained context_refs projection")
			}
			found := false
			for _, m := range model.messages[1] {
				if m.ToolCallID == "list" {
					var page refbridge.Page
					if err := json.Unmarshal([]byte(m.Content), &page); err != nil {
						t.Error(err)
						continue
					}
					found = len(page.Evidence) == 1 && page.Evidence[0].ReceiptID == receipt.ID && page.SessionID == referenceScope(cfg)
				}
			}
			if !found {
				t.Error("child queried another actor's evidence")
			}
		}()
	}
	wg.Wait()
	if parentCalls.Load() != 0 {
		t.Fatal("evidence child inherited parent binding")
	}
}
