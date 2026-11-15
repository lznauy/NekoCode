package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type reviewCase struct {
	name, request, diff      string
	files                    map[string]string
	targetPath, targetSymbol string
	defective                bool
}

func reviewCases() []reviewCase {
	cleanup := `function useChat() {
  const [msgs, setMsgs] = useState([]);
  const setMessages = (next) => {
    resetBuffers();
    setMsgs(next);
    setText('');
    setError(null);
    sidRef.current = null;
    userSidRef.current = null;
    sendingRef.current = false;
    abortedRef.current = false;
  };
  const clearMessages = () => setMessages([]);
  return { clearMessages };
}
`
	caller := `async function deleteCurrentSession() {
  await deleteSession(currentId);
  chat.clearMessages();
}
`
	gui := `function permissionDetails(approval) {
  return approval.capabilities.map(friendlyCapability).join('、');
}
`
	tui := `package example
import "strings"
type Approval struct { Capabilities []string }
func permissionSummary(approval Approval) string {
  lines := []string{"需要确认"}
  if len(approval.Capabilities) > 0 {
    lines = append(lines, "权限: " + strings.Join(approval.Capabilities, "、"))
  }
  return strings.Join(lines, "\n")
}
`
	return []reviewCase{
		{"delegated_cleanup", "有人认为 clearMessages 简化后丢失状态清理。请用 check 独立审查本次改动，只报告确认的缺陷，不修改文件。", "- const clearMessages = () => { resetBuffers(); setMsgs([]); setText(''); setError(null); sidRef.current = null; userSidRef.current = null; sendingRef.current = false; abortedRef.current = false; };\n+ const clearMessages = () => setMessages([]);", map[string]string{"chat.ts": cleanup, "app.ts": caller}, "chat.ts", "clearMessages", false},
		{"missing_cleanup", "有人认为 clearMessages 简化后丢失状态清理。请用 check 独立审查本次改动，只报告确认的缺陷，不修改文件。", "- const clearMessages = () => { resetBuffers(); setMsgs([]); setText(''); setError(null); sidRef.current = null; userSidRef.current = null; sendingRef.current = false; abortedRef.current = false; };\n+ const clearMessages = () => setMsgs([]);", map[string]string{"chat.ts": strings.Replace(cleanup, "const clearMessages = () => setMessages([]);", "const clearMessages = () => setMsgs([]);", 1), "app.ts": caller}, "chat.ts", "clearMessages", true},
		{"translated_capabilities", "有人认为 TUI 没有展示申请的 capability，而 GUI 有。请用 check 核实本次改动后两端是否都显示能力范围；只报告确认的缺陷，不修改文件。", "GUI: 将 capabilities 翻译为可读名称。\nTUI: 新增权限摘要函数 permissionSummary。", map[string]string{"gui.ts": gui, "confirm.go": tui}, "confirm.go", "permissionSummary", false},
		{"missing_capabilities", "有人认为 TUI 没有展示申请的 capability，而 GUI 有。请用 check 核实本次改动后两端是否都显示能力范围；只报告确认的缺陷，不修改文件。", "GUI: 将 capabilities 翻译为可读名称。\nTUI: 新增权限摘要函数 permissionSummary。", map[string]string{"gui.ts": gui, "confirm.go": strings.Replace(tui, "  if len(approval.Capabilities) > 0 {\n    lines = append(lines, \"权限: \" + strings.Join(approval.Capabilities, \"、\"))\n  }\n", "", 1)}, "confirm.go", "permissionSummary", true},
	}
}

type reviewEvidence struct {
	Path  string `json:"path"`
	Quote string `json:"quote"`
}
type reviewFinding struct {
	Path     string           `json:"path"`
	Symbol   string           `json:"symbol"`
	Trigger  string           `json:"trigger"`
	Reason   string           `json:"reason"`
	Evidence []reviewEvidence `json:"evidence"`
}
type reviewVerdict struct {
	Findings []reviewFinding `json:"findings"`
}
type reviewResult struct {
	Version             string           `json:"version"`
	Case                string           `json:"case"`
	Trial               int              `json:"trial"`
	Issues              []string         `json:"issues"`
	Actions             []behaviorAction `json:"actions"`
	Verdict             string           `json:"verdict_for_human_review"`
	InfrastructureError string           `json:"infrastructure_error,omitempty"`
}

type reviewFixture struct {
	tc    reviewCase
	read  map[string]bool
	check bool
}

func (f *reviewFixture) execute(call toolCall, manager *workflowSet) (string, error) {
	var args map[string]string
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", err
	}
	switch call.Function.Name {
	case "skill":
		sk, ok := manager.Get(args["name"])
		if !ok {
			return "", fmt.Errorf("unknown skill")
		}
		if sk.Name == "check" {
			f.check = true
		}
		return formatWorkflow(sk), nil
	case "read":
		data, ok := f.tc.files[args["path"]]
		if !ok {
			return "", fmt.Errorf("path is outside fixture")
		}
		f.read[args["path"]] = true
		return data, nil
	case "search":
		if args["query"] == "" {
			return "", fmt.Errorf("query required")
		}
		var matches []string
		for _, path := range reviewPaths(f.tc.files) {
			for i, line := range strings.Split(f.tc.files[path], "\n") {
				if strings.Contains(line, args["query"]) {
					matches = append(matches, fmt.Sprintf("%s:%d:%s", path, i+1, line))
				}
			}
		}
		return strings.Join(matches, "\n"), nil
	default:
		return "", fmt.Errorf("read-only review: tool not available")
	}
}

func reviewPaths(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func reviewTools() []toolDef {
	var result []toolDef
	for _, spec := range []struct{ name, description, field string }{
		{"skill", "Load check or hunt skill", "name"},
		{"read", "Read a complete fixture file from the supplied file list", "path"},
		{"search", "Literal substring search across every fixture file; returns path and line", "query"},
	} {
		result = append(result, toolDef{Type: "function", Function: functionDef{Name: spec.name, Description: spec.description, Parameters: parameters{Type: "object", Properties: map[string]property{spec.field: {Type: "string"}}, Required: []string{spec.field}}}})
	}
	return result
}

func runReviewCase(t *testing.T, client chatModel, variant behaviorVariant, tc reviewCase) reviewResult {
	t.Helper()
	fixture := &reviewFixture{tc: tc, read: map[string]bool{}}
	result := reviewResult{Version: variant.version.Name, Case: tc.name, Issues: []string{}, Actions: []behaviorAction{}}
	messages := []message{
		{Role: "system", Content: variant.system},
		{Role: "user", Content: tc.request + "\n当前文件：" + strings.Join(reviewPaths(tc.files), ", ") + "\n改动摘要（不是完整源码）：\n" + tc.diff + `
最终回答仅输出 JSON：{"findings":[{"path":"文件路径","symbol":"缺陷所在符号","trigger":"触发条件","reason":"实际路径、违反的行为约定及影响","evidence":[{"path":"证据文件","quote":"从已读取源码逐字引用的非空片段"}]}]}。没有确认的缺陷则 findings 为 []。`},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	for step := 0; step < 16; step++ {
		response, err := client.Chat(ctx, messages, reviewTools())
		if err != nil {
			result.InfrastructureError = fmt.Sprintf("model request failed (%T); provider text omitted", err)
			return result
		}
		if response == nil || len(response.Choices) == 0 {
			result.InfrastructureError = "model returned no choices"
			return result
		}
		answer := response.Choices[0].Message
		messages = append(messages, answer)
		if len(answer.ToolCalls) == 0 {
			result.Verdict = answer.Content
			break
		}
		for _, call := range answer.ToolCalls {
			output, err := fixture.execute(call, variant.manager)
			result.Actions = append(result.Actions, behaviorAction{Tool: call.Function.Name, Arguments: call.Function.Arguments, Failed: err != nil})
			if err != nil {
				output = err.Error()
			}
			messages = append(messages, message{Role: "tool", ToolCallID: call.ID, Content: output, IsError: err != nil})
		}
	}
	result.Issues = scoreReview(fixture, result.Verdict)
	return result
}

func scoreReview(f *reviewFixture, answer string) []string {
	issues := []string{}
	if !f.check {
		issues = append(issues, "check_not_loaded")
	}
	for _, path := range reviewPaths(f.tc.files) {
		if !f.read[path] {
			issues = append(issues, "source_not_read:"+path)
		}
	}
	var verdict reviewVerdict
	if json.Unmarshal([]byte(answer), &verdict) != nil || verdict.Findings == nil {
		return append(issues, "invalid_verdict")
	}
	found := false
	for _, finding := range verdict.Findings {
		if finding.Path == f.tc.targetPath && finding.Symbol == f.tc.targetSymbol {
			found = true
		} else {
			issues = append(issues, "unexpected_finding")
		}
		if strings.TrimSpace(finding.Trigger) == "" || strings.TrimSpace(finding.Reason) == "" || len(finding.Evidence) == 0 {
			issues = append(issues, "incomplete_evidence_chain")
		}
		for _, evidence := range finding.Evidence {
			source, exists := f.tc.files[evidence.Path]
			if !exists || !f.read[evidence.Path] || strings.TrimSpace(evidence.Quote) == "" || !strings.Contains(source, evidence.Quote) {
				issues = append(issues, "unsupported_evidence")
			}
		}
	}
	if !f.tc.defective && len(verdict.Findings) > 0 {
		issues = append(issues, "false_positive")
	}
	if f.tc.defective && !found {
		issues = append(issues, "false_negative")
	}
	return issues
}

// Opt-in only; the ordinary offline suite never contacts a model endpoint.
func TestReviewBehaviorEval(t *testing.T) {
	model := os.Getenv("NEKOCODE_PROMPT_EVAL_MODEL")
	if model == "" {
		t.Skip("live model eval disabled")
	}
	baseURL, key := os.Getenv("NEKOCODE_PROMPT_EVAL_BASE_URL"), os.Getenv("NEKOCODE_PROMPT_EVAL_API_KEY")
	if baseURL == "" || key == "" {
		t.Fatal("live eval requires explicit BASE_URL and API_KEY")
	}
	repeats := 1
	if value := os.Getenv("NEKOCODE_PROMPT_EVAL_REPEATS"); value != "" {
		var err error
		repeats, err = strconv.Atoi(value)
		if err != nil || repeats < 1 || repeats > 5 {
			t.Fatal("REPEATS must be between 1 and 5")
		}
	}
	ref := os.Getenv("NEKOCODE_PROMPT_EVAL_BASE_REF")
	if ref == "" {
		ref = "HEAD"
	}
	baseline, commit := baselineBehaviorVariant(t, ref)
	current := currentBehaviorVariant(t)
	if baseline.version.SystemSHA256 == current.version.SystemSHA256 && maps.Equal(baseline.version.SkillSHA256, current.version.SkillSHA256) {
		t.Fatal("baseline matches current instructions")
	}
	client := newModel(modelConfig{Model: model, BaseURL: baseURL, APIKey: key, MaxTokens: 4096})
	results := []reviewResult{}
	for repeat := 0; repeat < repeats; repeat++ {
		for i, tc := range reviewCases() {
			variants := []behaviorVariant{baseline, current}
			if (repeat+i)%2 != 0 {
				slices.Reverse(variants)
			}
			for _, variant := range variants {
				result := runReviewCase(t, client, variant, tc)
				result.Trial = repeat + 1
				results = append(results, result)
			}
		}
	}
	report, err := json.Marshal(struct {
		Model          string            `json:"model"`
		BaselineCommit string            `json:"baseline_commit"`
		Versions       []behaviorVersion `json:"versions"`
		Results        []reviewResult    `json:"results"`
	}{model, commit, []behaviorVersion{baseline.version, current.version}, results})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("REVIEW_COMPARISON %s", report)
	for _, result := range results {
		if result.InfrastructureError != "" {
			t.Errorf("%s/%s: %s", result.Version, result.Case, result.InfrastructureError)
		} else if result.Version == "current" && len(result.Issues) > 0 {
			t.Errorf("current/%s: %v", result.Case, result.Issues)
		}
	}
}
