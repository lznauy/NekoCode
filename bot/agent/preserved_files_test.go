package agent

import (
	"errors"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/read"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
)

func preservedTestAgent(t *testing.T, dir string) *Agent {
	t.Helper()
	a := newTestAgent()
	a.deps.toolRegistry.Register(&read.ReadTool{})
	a.deps.toolRegistry.Workspace().Configure(dir, nil)
	a.deps.toolExecutor.SetPermissionPolicy(permission.PermissionsDecl{}, dir, dir)
	return a
}

func TestPreservedFileCloseout(t *testing.T) {
	for _, outcome := range []string{"unchanged", "restored", "unresolved", "deleted"} {
		t.Run(outcome, func(t *testing.T) {
			dir := t.TempDir()
			a := preservedTestAgent(t, dir)
			path := filepath.Join(dir, "archive.json")
			write := func(data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("original")
			a.loopRunner.startRun("保留原件：archive.json、archive.json", nil)
			if len(a.run.preserved) != 1 || a.run.preserved[0].SHA256 == "" {
				t.Fatal(a.run.preserved)
			}
			finish := func() bool {
				return a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "done"}, nil)
			}
			if outcome == "unchanged" {
				if !finish() || a.run.finalText != "done" {
					t.Fatal(a.run.finalText)
				}
				return
			}
			if outcome == "deleted" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				write("modified")
			}
			if finish() || a.run.finalText != "" || len(a.run.pendingHints) != 1 {
				t.Fatal("premature closeout")
			}
			if outcome == "restored" {
				write("original")
			}
			if !finish() || strings.Contains(a.run.finalText, "原件校验未通过") != (outcome != "restored") {
				t.Fatal(a.run.finalText)
			}
		})
	}
}

func TestPreservedFilesRequireExplicitPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "outside")); err != nil {
		t.Skip(err)
	}
	a := preservedTestAgent(t, dir)
	a.capturePreservedFiles("Keep the original sample.json unchanged.\nPreserve files: ../private, outside/file, missing")
	files := a.run.preserved
	if len(files) != 3 {
		t.Fatal(files)
	}
	for _, file := range files {
		if file.SHA256 != "" {
			t.Fatal(file)
		}
	}
}

func TestPreservedFilePermissionsAtBaselineAndCloseout(t *testing.T) {
	for _, decl := range []permission.PermissionsDecl{
		{Deny: []string{"read(/archive.json)"}},
		{Ask: []string{"read(/archive.json)"}},
	} {
		dir := t.TempDir()
		a := preservedTestAgent(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "archive.json"), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		a.deps.toolExecutor.SetPermissionPolicy(decl, dir, dir)
		capture := func() { a.loopRunner.startRun("Preserve files: archive.json", nil) }
		capture()
		if got := a.run.preserved; len(got) != 1 || got[0].SHA256 != "" {
			t.Fatalf("captured restricted baseline: %+v", got)
		}
		a.deps.toolExecutor.SetPermissionPolicy(permission.PermissionsDecl{}, dir, dir)
		capture()
		if a.run.preserved[0].SHA256 == "" {
			t.Fatal("missing permitted baseline")
		}
		a.deps.toolExecutor.SetPermissionPolicy(decl, dir, dir)
		finished := a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "done"}, nil)
		if !finished || !strings.Contains(a.run.finalText, "无法校验原件") || len(a.run.pendingHints) != 0 {
			t.Fatalf("closeout used revoked authority: %q", a.run.finalText)
		}
	}
}

func TestPreservedFileRejectsSymlinksWithinWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "private"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private", "source.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	for alias, target := range map[string]string{"file.txt": "private/source.txt", "alias": "private"} {
		if err := os.Symlink(filepath.FromSlash(target), filepath.Join(dir, alias)); err != nil {
			t.Skip(err)
		}
	}
	a := preservedTestAgent(t, dir)
	a.deps.toolExecutor.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"read(/private/**)"}}, dir, dir)
	a.capturePreservedFiles("Preserve files: file.txt, alias/source.txt")
	files := a.run.preserved
	if len(files) != 2 || files[0].SHA256 != "" || files[1].SHA256 != "" {
		t.Fatalf("followed symlink: %+v", files)
	}
}

func TestPreservedFileSynthesisReportsUnresolvedDifference(t *testing.T) {
	dir := t.TempDir()
	a := preservedTestAgent(t, dir)
	a.deps.llmClient = &executionLLM{}
	path := filepath.Join(dir, "original.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	a.loopRunner.startRun("Preserve files: original.txt", nil)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if text := a.modelRunner.synthesize(); !strings.Contains(text, "原件校验未通过") || a.run.finalText != text || len(a.run.pendingHints) != 0 {
		t.Fatalf("terminal synthesis lost preservation status: %q", text)
	}
}

func TestPreservedFileOnHookCloseout(t *testing.T) {
	for _, end := range []string{"completed hook", "hint limit"} {
		t.Run(end, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "archive")
			if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			a := preservedTestAgent(t, dir)
			a.loopRunner.startRun("Preserve files: archive", nil)
			if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := a.deps.gov.Register(policy.Hook{Name: "closeout", Point: policy.Stop, On: func(policy.State) *policy.Result {
				if end == "hint limit" {
					return &policy.Result{Hint: &policy.Hint{Content: "review"}}
				}
				stop := policy.StopCompleted
				return &policy.Result{Stop: &stop}
			}}); err != nil {
				t.Fatal(err)
			}
			a.run.consecutiveHints = maxConsecutiveHints - 1
			if !a.turnRunner.handleText(&reasoningResult{Action: actionChat, ActionInput: "done"}, nil) ||
				!strings.Contains(a.run.finalText, "原件校验未通过：") || len(a.run.pendingHints) != 0 {
				t.Fatalf("terminal response or queued retry: %q %+v", a.run.finalText, a.run.pendingHints)
			}
		})
	}
}

func TestPreserveNestedDocumentedPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "archive"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "archive", "source"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	a := preservedTestAgent(t, dir)
	a.capturePreservedFiles("Preserve files: archive/source")
	files := a.run.preserved
	if len(files) != 1 || files[0].SHA256 == "" {
		t.Fatalf("nested portable path: %+v", files)
	}
}

func TestSteeringAddsOriginalWithoutReplacingBaseline(t *testing.T) {
	dir := t.TempDir()
	a := preservedTestAgent(t, dir)
	for _, name := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.loopRunner.startRun("Preserve files: first", nil)
	if err := os.WriteFile(filepath.Join(dir, "first"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.TrySteer("保留原件：first、second"); err != nil {
		t.Fatal(err)
	}
	if err := a.drainSteering(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "second"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	text := "done"
	a.turnRunner.reviewPreservedFiles(&text, false)
	if !strings.Contains(text, `"first", "second"`) {
		t.Fatal(text)
	}
	a.loopRunner.startRun("new unrelated request", nil)
	if len(a.run.preserved) != 0 {
		t.Fatal("new request inherited old baselines")
	}
}

func TestBaselineSaveFailureStopsBeforeModel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "original"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	a := preservedTestAgent(t, dir)
	llm := &fakeLLM{}
	a.deps.llmClient = llm
	cause := errors.New("cannot persist baseline")
	goal := &GoalPreservation{Save: func([]protocol.PreservedFile) error { return cause }}
	result := a.RunContext(WithGoalPreservation(t.Context(), goal), "Preserve files: original", nil, nil)
	if llm.calls != 0 || !errors.Is(result.Error, cause) || !protocol.EvidenceIncomplete(result.Error) {
		t.Fatalf("calls=%d result=%+v", llm.calls, result)
	}
}

func TestFinalSteeringLeavesNewOriginalPendingReview(t *testing.T) {
	a := preservedTestAgent(t, t.TempDir())
	a.deps.llmClient = &fakeLLM{tokens: []types.StreamToken{{Content: "Verified.\nGOAL DONE"}}}
	goal := new(GoalPreservation)
	ctx := WithGoalReview(WithGoalPreservation(t.Context(), goal), func(string) string { return "" })
	result := a.RunContext(ctx, "check", nil, func(e protocol.StepEvent) {
		if e.Action == protocol.StepActionChat {
			if err := a.TrySteer("Preserve files: absent.txt"); err != nil {
				t.Fatal(err)
			}
		}
	})
	if result.Error != nil || len(goal.Files) != 1 || !goal.NeedsReview {
		t.Fatalf("result=%+v goal=%+v", result, goal)
	}
}
