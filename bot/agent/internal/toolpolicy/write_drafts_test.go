package toolpolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/read"
	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
	"nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/policy"
	"nekocode/bot/policy/builtin"
	textutil "nekocode/util/text"
)

func TestDraftRecoveryRequiresReadAndOriginalActorAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := workspace.New(filepath.Dir(path), nil)
	gov := policy.New()
	builtin.Register(gov)
	var drafts WriteDrafts
	proposal := core.ToolCallItem{Name: "write", Args: map[string]any{"path": path, "content": "replacement"}}
	rejected := drafts.Check(t.Context(), gov, manager, &proposal)
	if len(drafts.recent) != 1 || !strings.Contains(rejected.BlockReason, drafts.recent[0].id) {
		t.Fatalf("missing recovery instruction: %+v", rejected)
	}
	ref := drafts.recent[0].id
	retry := func(owner *WriteDrafts, target string) (Decision, core.ToolCallItem) {
		call := core.ToolCallItem{Name: "write", Args: map[string]any{"path": target, "reuse": ref}}
		return owner.Check(t.Context(), gov, manager, &call), call
	}
	if d, _ := retry(&drafts, path); !strings.Contains(d.BlockReason, ref) || len(drafts.recent) != 1 {
		t.Fatalf("premature retry: %+v", d)
	}
	gov.RecordTool(policy.ToolResult{Name: "read", Args: map[string]any{"path": path}})
	if d, _ := retry(&WriteDrafts{}, path); d.BlockReason == "" {
		t.Fatal("cross-run reuse allowed")
	}
	if d, _ := retry(&drafts, path+".other"); d.BlockReason == "" {
		t.Fatal("wrong path allowed")
	}
	d, recovered := retry(&drafts, path)
	if d.BlockReason != "" || recovered.Args["content"] != "replacement" || recovered.Args["_existing_draft"] != true {
		t.Fatalf("recovery failed: %+v %+v", d, recovered)
	}
	state := execution.NewExecutionState()
	ctx := execution.WithExecutionState(testutil.Context(filepath.Dir(path)), state)
	writer := &write.WriteTool{}
	if _, err := writer.Execute(ctx, recovered.Args); err == nil {
		t.Fatal("governance ledger alone authorized draft")
	}
	out, err := (&read.ReadTool{}).Execute(ctx, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	state.Reads.Observe(map[string]string{out.ReadID: textutil.NormalizeTerminalOutput(out.Render())})
	if _, err := writer.Execute(ctx, recovered.Args); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "replacement" {
		t.Fatalf("draft output %q: %v", data, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Execute(ctx, recovered.Args); err == nil {
		t.Fatal("draft recreated deleted target")
	}
}

func TestOtherRejectionsDoNotOfferDraftReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	manager := workspace.New(filepath.Dir(path), nil)
	gov := policy.New()
	builtin.Register(gov)
	gov.Register(policy.Hook{Name: "additional_guard", Point: policy.PreToolUse, On: func(policy.State) *policy.Result {
		return &policy.Result{BlockTool: &policy.BlockTool{Tool: "write", Reason: "blocked by another guard"}}
	}})
	var drafts WriteDrafts
	call := core.ToolCallItem{Name: "write", Args: map[string]any{"path": path, "content": "new"}}
	decision := drafts.Check(t.Context(), gov, manager, &call)
	if decision.BlockReason == "" || len(drafts.recent) != 0 {
		t.Fatalf("unexpected decision or saved draft: %+v", decision)
	}
}
