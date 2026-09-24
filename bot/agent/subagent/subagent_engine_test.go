package subagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/agent/internal/toolpolicy"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/policy"
	"nekocode/bot/policy/builtin"
	"nekocode/bot/prompt"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

type scriptedLLM struct {
	scripts  [][]types.StreamToken
	calls    int
	messages [][]types.Message
	tools    [][]types.ToolDef
}

func (f *scriptedLLM) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	panic("Chat should not be called by subagent")
}

func (f *scriptedLLM) ChatStream(_ context.Context, messages []types.Message, toolDefs []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	index := f.calls
	f.calls++
	f.messages = append(f.messages, append([]types.Message(nil), messages...))
	f.tools = append(f.tools, append([]types.ToolDef(nil), toolDefs...))
	tokens := make(chan types.StreamToken, len(f.scripts[index]))
	errs := make(chan error, 1)
	for _, token := range f.scripts[index] {
		tokens <- token
	}
	close(tokens)
	errs <- nil
	close(errs)
	return tokens, errs
}

func (f *scriptedLLM) SetMaxTokens(int)         {}
func (f *scriptedLLM) GetMaxTokens() int        { return 0 }
func (f *scriptedLLM) SetDisableThinking(bool)  {}
func (f *scriptedLLM) GetDisableThinking() bool { return false }

type testProxyTool struct{ name string }

func (t testProxyTool) Name() string        { return t.name }
func (t testProxyTool) Description() string { return "test proxy" }
func (t testProxyTool) Parameters() []core.Parameter {
	return nil
}
func (t testProxyTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeParallel
}
func (t testProxyTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

type countingTool struct {
	name  string
	calls int
}

func (t *countingTool) Name() string        { return t.name }
func (t *countingTool) Description() string { return "counting test tool" }
func (t *countingTool) Parameters() []core.Parameter {
	return nil
}
func (t *countingTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeParallel
}
func (t *countingTool) Execute(context.Context, map[string]any) (string, error) {
	t.calls++
	return "ok", nil
}

func TestBuildTaskPromptKeepsHandoffOutOfSystemRole(t *testing.T) {
	cfg := RunConfig{
		Profile: Profile{
			Name:         "coder",
			SystemPrompt: "base prompt",
		},
		Prompt:              "current task",
		Handoff:             "prior findings",
		SkillContents:       []string{"<skill_content name=\"check\">review workflow</skill_content>"},
		ProjectInstructions: "\n\nProject instructions from NEKOCODE.md: use project conventions",
	}

	system := buildSystemPrompt(cfg)
	if !strings.Contains(system, cfg.ProjectInstructions) {
		t.Fatal("delegated agent lost project instructions")
	}
	if strings.Contains(system, "prior findings") || strings.Contains(system, "current task") {
		t.Fatalf("task evidence leaked into system prompt: %q", system)
	}
	if strings.Contains(system, "review workflow") {
		t.Fatalf("task skill was promoted to system authority: %q", system)
	}
	workflow := buildSkillWorkflow(cfg)
	for _, want := range []string{"Task-scoped skills", "review workflow"} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("workflow = %q, want %q", workflow, want)
		}
	}
	got := buildTaskPrompt(cfg)
	for _, want := range []string{"unverified evidence", "prior findings", "[Current delegated task]", "current task"} {
		if !strings.Contains(got, want) {
			t.Fatalf("task prompt = %q, want %q", got, want)
		}
	}
	if strings.Contains(system, "<cwd>") {
		t.Fatalf("volatile cwd leaked into stable system prompt: %q", system)
	}
}

func TestRunRequiresStructuredResultSubmission(t *testing.T) {
	llm := &scriptedLLM{scripts: [][]types.StreamToken{
		{{Content: "I have enough to answer."}},
		{{ToolCallDelta: &types.ToolCallDelta{
			Index: 0, ID: "result-1", Name: submitResultToolName,
			Arguments: `{"summary":"完整结论","evidence":["关键证据"],"files":["main.go"],"verification":"go test passed","unfinished":[],"risks":[]}`,
		}}},
	}}
	engine := New(Config{LLM: llm, Tools: tools.New()})

	result, err := engine.Run(context.Background(), RunConfig{
		Prompt:  "inspect the package",
		Profile: Profile{Name: "explore", SystemPrompt: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 2 {
		t.Fatalf("LLM calls = %d, want 2", llm.calls)
	}
	if result.Status != StatusCompleted || result.Handoff == nil || result.Handoff.Summary != "完整结论" {
		t.Fatalf("result = %+v", result)
	}
	for _, want := range []string{"完整结论", "关键证据", "go test passed"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("formatted handoff = %q, missing %q", result.Content, want)
		}
	}
	if !toolDefsContain(llm.tools[0], submitResultToolName) {
		t.Fatalf("completion tool missing from tool defs: %+v", llm.tools[0])
	}
	if !messagesContainText(llm.messages[1], "I have enough to answer.") ||
		!messagesContainText(llm.messages[1], submitResultToolName) {
		t.Fatalf("protocol feedback missing from retry context: %+v", llm.messages[1])
	}
}

func TestRunReportsPartialAfterCompletionProtocolRetries(t *testing.T) {
	llm := &scriptedLLM{scripts: [][]types.StreamToken{
		{{Content: "first progress message"}},
		{{Content: "second progress message"}},
		{{Content: "third progress message"}},
	}}
	engine := New(Config{LLM: llm, Tools: tools.New()})

	result, err := engine.Run(context.Background(), RunConfig{
		Prompt:  "inspect the package",
		Profile: Profile{Name: "explore", SystemPrompt: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != maxCompletionProtocolRetries+1 {
		t.Fatalf("LLM calls = %d, want %d", llm.calls, maxCompletionProtocolRetries+1)
	}
	if result.Status != StatusPartial {
		t.Fatalf("status = %v, want partial", result.Status)
	}
	for _, want := range []string{"did not submit a structured result", "third progress message"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("partial result = %q, missing %q", result.Content, want)
		}
	}
}

func TestRunRejectsMixedSubmitBatchWithoutExecutingTools(t *testing.T) {
	llm := &scriptedLLM{scripts: [][]types.StreamToken{
		{
			{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "result-1", Name: submitResultToolName, Arguments: `{"summary":"premature","evidence":[],"files":[],"verification":"not run","unfinished":[],"risks":[]}`}},
			{ToolCallDelta: &types.ToolCallDelta{Index: 1, ID: "inspect-1", Name: "inspect", Arguments: `{}`}},
		},
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "result-2", Name: submitResultToolName, Arguments: `{"summary":"complete","evidence":[],"files":[],"verification":"not run","unfinished":[],"risks":[]}`}}},
	}}
	inspect := &countingTool{name: "inspect"}
	engine := New(Config{LLM: llm, Tools: tools.New(inspect)})

	result, err := engine.Run(context.Background(), RunConfig{
		Prompt:  "inspect the package",
		Profile: Profile{Name: "explore", SystemPrompt: "test", Tools: []string{"inspect"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompleted || result.Handoff == nil || result.Handoff.Summary != "complete" {
		t.Fatalf("result = %+v", result)
	}
	if inspect.calls != 0 {
		t.Fatalf("mixed completion batch executed %d ordinary tools", inspect.calls)
	}
	for _, callID := range []string{"result-1", "inspect-1"} {
		if !messagesContainToolResult(llm.messages[1], callID) {
			t.Fatalf("protocol rejection missing tool result for %s: %+v", callID, llm.messages[1])
		}
	}
}

func TestRunRejectsIncompleteStructuredHandoff(t *testing.T) {
	llm := &scriptedLLM{scripts: [][]types.StreamToken{
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "result-1", Name: submitResultToolName, Arguments: `{"summary":"only a summary"}`}}},
		{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "result-2", Name: submitResultToolName, Arguments: `{"summary":"complete","evidence":[],"files":[],"verification":"not run","unfinished":[],"risks":[]}`}}},
	}}
	engine := New(Config{LLM: llm, Tools: tools.New()})

	result, err := engine.Run(context.Background(), RunConfig{
		Prompt:  "inspect the package",
		Profile: Profile{Name: "explore", SystemPrompt: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompleted || result.Handoff == nil || result.Handoff.Summary != "complete" {
		t.Fatalf("result = %+v", result)
	}
	if !messagesContainToolResult(llm.messages[1], "result-1") ||
		!messagesContainText(llm.messages[1], "requires field evidence to be an array") {
		t.Fatalf("missing structured-handoff rejection in retry context: %+v", llm.messages[1])
	}
}

func TestRunRejectsReservedCompletionToolCollision(t *testing.T) {
	collision := &countingTool{name: submitResultToolName}
	engine := New(Config{LLM: &scriptedLLM{}, Tools: tools.New(collision)})

	result, err := engine.Run(context.Background(), RunConfig{
		Prompt:  "inspect the package",
		Profile: Profile{Name: "explore", SystemPrompt: "test"},
	})
	if err == nil || !strings.Contains(err.Error(), "reserved lifecycle tool name") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if collision.calls != 0 {
		t.Fatalf("reserved-name collision tool executed %d times", collision.calls)
	}
}

func toolDefsContain(defs []types.ToolDef, name string) bool {
	for _, def := range defs {
		if def.Function.Name == name {
			return true
		}
	}
	return false
}

func messagesContainToolResult(messages []types.Message, callID string) bool {
	for _, message := range messages {
		if message.ToolCallID == callID && message.IsError {
			return true
		}
	}
	return false
}

func TestContextManagerRefreshesSandboxAtBuildTime(t *testing.T) {
	root := "/repo"
	cfg := RunConfig{
		Profile: Profile{
			Name:         "coder",
			SystemPrompt: "base prompt",
		},
		Environment: func() prompt.Environment {
			return prompt.Environment{
				Cwd: root, Roots: []prompt.Root{{Path: root, Access: "read-write"}},
			}
		},
	}

	e := &Engine{}
	mgr := e.newContextManager(cfg)
	first := mgr.BuildRequest(ctxmgr.ModelRequest{})
	if !messagesContainText(first, "<environment_context>") || !messagesContainText(first, `<root access="read-write">/repo</root>`) {
		t.Fatalf("first context missing environment block: %+v", first)
	}
	root = "/approved"
	second := mgr.BuildRequest(ctxmgr.ModelRequest{})
	if !messagesContainText(second, `<root access="read-write">/repo</root>`) || !messagesContainText(second, `<root access="read-write">/approved</root>`) {
		t.Fatalf("environment block did not refresh: %+v", second)
	}
	for i := range first {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content {
			t.Fatalf("environment refresh rewrote request prefix at %d: first=%+v second=%+v", i, first[i], second[i])
		}
	}
	if strings.Contains(mgr.Snapshot().SystemPrompt, "<environment_context>") {
		t.Fatal("subagent environment leaked into snapshot")
	}
}

func TestBuildSystemPromptNilWorkspace(t *testing.T) {
	cfg := RunConfig{
		Profile: Profile{Name: "coder", SystemPrompt: "base prompt"},
	}
	got := buildSystemPrompt(cfg)
	if strings.Contains(got, "<environment_context>") {
		t.Fatalf("nil workspace should not inject environment block: %q", got)
	}
}

func messagesContainText(msgs []types.Message, text string) bool {
	for _, msg := range msgs {
		if strings.Contains(msg.Content, text) {
			return true
		}
	}
	return false
}

func TestExecuteToolBatchRejectsToolOutsideProfile(t *testing.T) {
	registry := tools.New()
	engine := &Engine{toolRegistry: registry}
	mgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "test"})
	executor := runner.NewExecutor(registry)
	var events []ToolCallEvent
	cfg := RunConfig{
		Profile: Profile{Name: "explore", Tools: []string{"read"}},
		OnToolCall: func(ev ToolCallEvent) {
			events = append(events, ev)
		},
	}
	state := newRunState()
	engine.executeToolBatch(context.Background(), cfg, mgr, executor, []core.ToolCallItem{{
		ID: "1", Name: "write", Args: map[string]any{"path": "blocked.txt"},
	}}, state, func(string) {}, func(string, ...any) {})

	if len(events) != 1 || events[0].Action != protocol.StepActionToolBlocked {
		t.Fatalf("events = %+v, want one blocked event", events)
	}
	if !strings.Contains(events[0].Output, `profile "explore"`) {
		t.Fatalf("blocked reason = %q", events[0].Output)
	}
}

func TestExecuteToolBatchRequiresAndReportsEffectiveTarget(t *testing.T) {
	registry := tools.New()
	registry.RegisterWithOptions(testProxyTool{name: "capability"}, tools.RegistrationOptions{
		ResolveTarget: func(args map[string]any) (tools.CallTarget, bool) {
			return tools.CallTarget{Name: "mcp__demo__lookup", Args: args}, true
		},
	})
	engine := &Engine{toolRegistry: registry}
	call := core.ToolCallItem{ID: "1", Name: "capability", Args: map[string]any{"query": "x"}}

	var denied []ToolCallEvent
	deniedCfg := RunConfig{
		Profile:    Profile{Name: "limited", Tools: []string{"capability"}},
		OnToolCall: func(ev ToolCallEvent) { denied = append(denied, ev) },
	}
	engine.executeToolBatch(context.Background(), deniedCfg,
		ctxmgr.New(ctxmgr.Config{SystemPrompt: "test"}), runner.NewExecutor(registry),
		[]core.ToolCallItem{call}, newRunState(), func(string) {}, func(string, ...any) {})
	if len(denied) != 1 || denied[0].Action != protocol.StepActionToolBlocked || denied[0].ToolName != "mcp__demo__lookup" {
		t.Fatalf("denied events = %+v", denied)
	}

	var allowed []ToolCallEvent
	allowedCfg := RunConfig{
		Profile:    Profile{Name: "limited", Tools: []string{"capability", "mcp__demo__lookup"}},
		OnToolCall: func(ev ToolCallEvent) { allowed = append(allowed, ev) },
	}
	engine.executeToolBatch(context.Background(), allowedCfg,
		ctxmgr.New(ctxmgr.Config{SystemPrompt: "test"}), runner.NewExecutor(registry),
		[]core.ToolCallItem{call}, newRunState(), func(string) {}, func(string, ...any) {})
	if len(allowed) != 2 || allowed[0].ToolName != "mcp__demo__lookup" || allowed[1].ToolName != "mcp__demo__lookup" {
		t.Fatalf("allowed events = %+v", allowed)
	}
}

func TestExecuteToolBatchDoesNotTrustAnotherContextsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.go")
	if err := os.WriteFile(path, []byte("package existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shared := policy.New()
	builtin.Register(shared)
	shared.RecordTool(policy.ToolResult{Name: "read", Args: map[string]any{"path": path}, Output: "ok"})
	guard := policy.New()
	builtin.Register(guard)

	registry := tools.New()
	engine := &Engine{toolRegistry: registry}
	var events []ToolCallEvent
	cfg := RunConfig{
		Profile: Profile{Name: "coder", Tools: []string{"edit"}},
		Policy:  shared, guard: guard,
		OnToolCall: func(ev ToolCallEvent) { events = append(events, ev) },
	}
	engine.executeToolBatch(context.Background(), cfg,
		ctxmgr.New(ctxmgr.Config{SystemPrompt: "test"}), runner.NewExecutor(registry),
		[]core.ToolCallItem{{ID: "1", Name: "edit", Args: map[string]any{"path": path}}},
		newRunState(), func(string) {}, func(string, ...any) {})
	if len(events) != 1 || events[0].Action != protocol.StepActionToolBlocked || !strings.Contains(events[0].Output, "请先 Read") {
		t.Fatalf("events = %+v, want actor-local read-before-write block", events)
	}
}

func TestExecuteToolBatchAllowsOwnReadWithSharedAuditPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.go")
	if err := os.WriteFile(path, []byte("package existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shared := policy.New()
	builtin.Register(shared)
	guard := newRunGuard()
	guard.RecordTool(policy.ToolResult{Name: "read", Args: map[string]any{"path": path}, Output: "ok"})

	registry := tools.New()
	registry.Register(testProxyTool{name: "edit"})
	engine := &Engine{toolRegistry: registry}
	var events []ToolCallEvent
	cfg := RunConfig{
		Profile: Profile{Name: "coder", Tools: []string{"edit"}},
		Policy:  shared, guard: guard,
		OnToolCall: func(ev ToolCallEvent) { events = append(events, ev) },
	}
	engine.executeToolBatch(context.Background(), cfg,
		ctxmgr.New(ctxmgr.Config{SystemPrompt: "test"}), runner.NewExecutor(registry),
		[]core.ToolCallItem{{ID: "1", Name: "edit", Args: map[string]any{"path": path}}},
		newRunState(), func(string) {}, func(string, ...any) {})
	if len(events) != 2 || events[0].Action != protocol.StepActionToolStart || events[1].Action != protocol.StepActionExecuteTool {
		t.Fatalf("events = %+v, want own read to allow edit despite shared audit policy", events)
	}
	snapshot := shared.Snapshot()
	if len(snapshot.ReadFiles) != 0 || snapshot.ToolEventCount != 1 {
		t.Fatalf("shared audit snapshot = %+v", snapshot)
	}
}

func TestSubagentPolicyBlockRequiresReadBeforeEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.go")
	if err := os.WriteFile(path, []byte("package existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gov := policy.New()
	builtin.Register(gov)
	call := core.ToolCallItem{Name: "edit", Args: map[string]any{"path": path}}
	if reason := toolpolicy.Check(gov, call).BlockReason; !strings.Contains(reason, "请先 Read") {
		t.Fatalf("reason = %q, want read-before-write block", reason)
	}
	gov.RecordTool(policy.ToolResult{Name: "read", Args: map[string]any{"path": path}, Output: "ok"})
	if reason := toolpolicy.Check(gov, call).BlockReason; reason != "" {
		t.Fatalf("reason after dedicated read = %q, want allow", reason)
	}
}

func TestReasonEmitsOnlyNonEmptyMessages(t *testing.T) {
	for _, tc := range []struct {
		name, text, reasoning string
	}{
		{name: "tool_only"},
		{name: "reasoning_and_tool", reasoning: "thinking"},
		{name: "text_and_tool", text: "progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			llm := &scriptedLLM{scripts: [][]types.StreamToken{{
				{Content: tc.text, ReasoningContent: tc.reasoning},
				{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "tool", Name: submitResultToolName, Arguments: "{}"}},
			}}}
			engine := New(Config{LLM: llm, Tools: tools.New()})
			var messages []string
			var reasoning string
			cfg := RunConfig{
				Profile:     Profile{Name: "test", SystemPrompt: "test"},
				OnMessage:   func(text string) { messages = append(messages, text) },
				OnReasoning: func(text string) { reasoning += text },
			}
			calls, text, err := engine.reason(context.Background(), engine.newContextManager(cfg), nil, "", nil, nil, "", nil, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || text != tc.text || reasoning != tc.reasoning {
				t.Fatalf("calls=%v text=%q reasoning=%q", calls, text, reasoning)
			}
			if tc.text == "" {
				if len(messages) != 0 {
					t.Fatalf("empty response emitted messages: %#v", messages)
				}
			} else if len(messages) != 1 || messages[0] != tc.text {
				t.Fatalf("messages=%#v", messages)
			}
		})
	}
}

func TestDelegatedJevJudgmentsAndPreviews(t *testing.T) {
	registry := tools.New()
	shell := &countingTool{name: "shell"}
	web := &countingTool{name: "web_fetch"}
	registry.Register(shell)
	registry.Register(web)
	parent := runner.NewExecutor(registry)
	parent.SetProjectStore(t.TempDir())
	parent.SetPermissionMode(permission.ModeAuto)
	shellJudged, webJudged, executed := 0, 0, 0
	parent.SetBashAutoJudge(func(context.Context, string) (bool, bool) { shellJudged++; return false, true })
	parent.SetWebAutoJudge(func(context.Context, string) (bool, bool) { webJudged++; return true, true })
	parent.SetShellExecutedHook(func(string) { executed++ })
	var previews []ToolCallEvent
	var approvals []protocol.ConfirmRequest
	cfg := RunConfig{
		ConfigurePermissions: parent.ConfigureChildPermissions,
		ConfirmFn: func(req protocol.ConfirmRequest) protocol.ConfirmReply {
			approvals = append(approvals, req)
			return protocol.Deny()
		},
		OnToolCall: func(ev ToolCallEvent) { previews = append(previews, ev) },
	}
	engine := &Engine{toolRegistry: registry}
	child, finish := engine.newExecutor(cfg)
	defer finish()
	result := child.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "shell1", Name: "shell", Args: map[string]any{"command": "make all"}}})
	if len(result) != 1 || result[0].Error != "" || shell.calls != 1 || shellJudged != 1 || executed != 1 {
		t.Fatalf("shell delegation failed: results=%+v calls=%d judged=%d executed=%d", result, shell.calls, shellJudged, executed)
	}
	child.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "web1", Name: "web_fetch", Args: map[string]any{"url": "https://example.com"}}})
	if webJudged != 1 || web.calls != 0 || len(approvals) != 1 {
		t.Fatalf("web gate not inherited: judged=%d ran=%d approvals=%+v", webJudged, web.calls, approvals)
	}
	if len(previews) != 2 || previews[0].CallID != "shell1" || previews[0].Action != protocol.StepActionToolPreview || previews[0].Decision != protocol.ToolDecisionJevSafe || previews[1].CallID != "web1" || previews[1].Decision != protocol.ToolDecisionJevURLRisky {
		t.Fatalf("missing delegated previews: %+v", previews)
	}
}
