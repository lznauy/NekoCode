package evals

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/format"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt-in A/B decision eval, not an Agent/runtime end-to-end test. Set
// NEKOCODE_PROMPT_EVAL_MODEL, NEKOCODE_PROMPT_EVAL_BASE_URL and
// NEKOCODE_PROMPT_EVAL_API_KEY explicitly, then run:
// cd evals && go test -run TestPromptBehaviorEval -v -count=1 -timeout 30m
// BASE_REF optionally selects the old Git revision (default HEAD). REPEATS
// optionally selects 1..5 trials per case/version (default 1). Both use the
// NEKOCODE_PROMPT_EVAL_ prefix. This doubles calls compared with a single-version
// eval. JSON evidence is logged as BEHAVIOR_COMPARISON; no credentials or endpoint
// are included. Each version loads its own system prompt AND hunt/check skills.
func TestPromptBehaviorEval(t *testing.T) {
	model := os.Getenv("NEKOCODE_PROMPT_EVAL_MODEL")
	if model == "" {
		t.Skip("live model eval disabled; set NEKOCODE_PROMPT_EVAL_MODEL explicitly")
	}
	baseURL, key := os.Getenv("NEKOCODE_PROMPT_EVAL_BASE_URL"), os.Getenv("NEKOCODE_PROMPT_EVAL_API_KEY")
	if baseURL == "" || key == "" {
		t.Fatal("live eval requires explicit BASE_URL and API_KEY; application credentials are never inferred")
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
		t.Fatal("baseline matches current instructions; choose an older NEKOCODE_PROMPT_EVAL_BASE_REF")
	}
	client := newModel(modelConfig{Model: model, BaseURL: baseURL, APIKey: key, MaxTokens: 4096})
	report := behaviorComparison{Model: model, BaselineCommit: commit, Repeats: repeats, Versions: []behaviorVersion{baseline.version, current.version}}
	for repeat := 0; repeat < repeats; repeat++ {
		for i, tc := range behaviorCases() {
			// Alternate order to reduce systematic first/second-run bias.
			variants := []behaviorVariant{baseline, current}
			if (repeat+i)%2 != 0 {
				variants[0], variants[1] = variants[1], variants[0]
			}
			for _, variant := range variants {
				result := runBehaviorCase(t, client, variant, tc)
				result.Trial = repeat + 1
				report.Results = append(report.Results, result)
			}
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("BEHAVIOR_COMPARISON %s", encoded)
	for _, version := range report.Versions {
		passed, infrastructure := 0, 0
		issues := map[string]int{}
		for _, result := range report.Results {
			if result.Version != version.Name {
				continue
			}
			if result.InfrastructureError != "" {
				infrastructure++
				continue
			}
			if len(result.Issues) == 0 {
				passed++
			}
			for _, issue := range result.Issues {
				issues[issue]++
			}
		}
		t.Logf("%s: passed=%d/%d infrastructure_errors=%d issue_counts=%v", version.Name, passed, len(behaviorCases())*repeats, infrastructure, issues)
	}
	for _, result := range report.Results {
		if result.InfrastructureError != "" {
			t.Errorf("%s/%s trial %d: infrastructure error: %s", result.Version, result.Case, result.Trial, result.InfrastructureError)
		} else if result.Version == "current" && len(result.Issues) > 0 {
			t.Errorf("current/%s trial %d: %v", result.Case, result.Trial, result.Issues)
		}
	}
	// Baseline behavior failures are comparison data, not failures of the new
	// implementation. Neither passing once nor an improved count establishes
	// statistical significance; final reasoning and approval requests need review.
}

type behaviorCase struct {
	name, request, followup string
	fix                     bool
}

func behaviorCases() []behaviorCase {
	return []behaviorCase{
		{"diagnose_only", "TestAdd 报错：Add(2,3) 得到 -1，预期 5。只定位根因，不修改文件。", "", false},
		{"authorized_fix", "请使用 $check 检查并修复 TestAdd 的失败，完成必要修改并验证。", "", true},
		{"steering", "请修复 TestAdd 的失败，完成必要修改并验证。", "补充：保持 Add 的签名和已有测试，继续完成原任务。", true},
		{"disproved_hypothesis", "我怀疑是 Go 测试缓存导致 TestAdd 错误。请检查实际证据，确认根因，只诊断，不改文件。", "", false},
		{"recurrence", "上次声称修好了，但 Add(2,3) 仍返回 -1。请重新诊断并修复，留下能防止复发的测试并验证。", "", true},
	}
}

type behaviorVersion struct {
	Name         string            `json:"name"`
	SystemSHA256 string            `json:"system_sha256"`
	SkillSHA256  map[string]string `json:"skill_sha256"`
}

type behaviorVariant struct {
	version behaviorVersion
	system  string
	manager *workflowSet
}

type behaviorAction struct {
	Tool       string `json:"tool"`
	Arguments  string `json:"arguments"`
	Failed     bool   `json:"failed"`
	Reproduced bool   `json:"original_failure_observed"`
	Verified   bool   `json:"repair_verified"`
}

type behaviorResult struct {
	Version             string           `json:"version"`
	Case                string           `json:"case"`
	Trial               int              `json:"trial"`
	Issues              []string         `json:"issues"`
	InfrastructureError string           `json:"infrastructure_error,omitempty"`
	Actions             []behaviorAction `json:"actions"`
	FinalAnswer         string           `json:"final_answer_for_human_review"`
}

type behaviorComparison struct {
	Model          string            `json:"model"`
	BaselineCommit string            `json:"baseline_commit"`
	Repeats        int               `json:"repeats"`
	Versions       []behaviorVersion `json:"versions"`
	Results        []behaviorResult  `json:"results"`
}

func currentBehaviorVariant(t *testing.T) behaviorVariant {
	t.Helper()
	root := behaviorRepoRoot(t)
	system, err := os.ReadFile(filepath.Join(root, "bot/prompt/system_zh.md"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := loadWorkflows(filepath.Join(root, "bot/extension/skill/bundled"))
	if err != nil {
		t.Fatal(err)
	}
	return describeBehaviorVariant(t, "current", string(system), manager)
}

func behaviorRepoRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("NEKOCODE_PROMPT_EVAL_REPO")
	if root == "" {
		root = ".."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func baselineBehaviorVariant(t *testing.T, ref string) (behaviorVariant, string) {
	t.Helper()
	return behaviorVariantAt(t, behaviorRepoRoot(t), ref)
}

func behaviorVariantAt(t *testing.T, root, ref string) (behaviorVariant, string) {
	t.Helper()
	// Resolve untrusted ref text to a commit before using it in object paths.
	data, err := exec.Command("git", "-C", root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}").Output()
	if err != nil {
		t.Fatalf("resolve baseline revision: %v", err)
	}
	commit := strings.TrimSpace(string(data))
	read := func(path string) []byte {
		t.Helper()
		data, err := exec.Command("git", "-C", root, "show", commit+":"+path).Output()
		if err != nil {
			t.Fatalf("read baseline %s: %v", path, err)
		}
		return data
	}
	system := string(read("bot/prompt/system_zh.md"))
	dir := t.TempDir()
	for _, name := range []string{"hunt", "check"} {
		path := filepath.Join(dir, name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, read("bot/extension/skill/bundled/"+name+"/SKILL.md"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manager, err := loadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	return describeBehaviorVariant(t, "baseline", system, manager), commit
}

func describeBehaviorVariant(t *testing.T, name, system string, manager *workflowSet) behaviorVariant {
	t.Helper()
	version := behaviorVersion{Name: name, SystemSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(system))), SkillSHA256: map[string]string{}}
	for _, name := range []string{"hunt", "check"} {
		sk, ok := manager.Get(name)
		if !ok {
			t.Fatalf("missing %s workflow", name)
		}
		version.SkillSHA256[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(sk.Content)))
	}
	return behaviorVariant{version: version, system: system, manager: manager}
}

func runBehaviorCase(t *testing.T, client chatModel, variant behaviorVariant, tc behaviorCase) behaviorResult {
	t.Helper()
	fixture := newBehaviorFixture(t)
	result := behaviorResult{Version: variant.version.Name, Case: tc.name, Issues: []string{}, Actions: []behaviorAction{}}
	messages := []message{
		{Role: "system", Content: variant.system},
		{Role: "user", Content: "工作区只有 add.go 和 add_test.go；可用技能 hunt（诊断根因）、check（审查修改）。\n" + tc.request},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	finished, followupDelivered := false, tc.followup == ""
	for step := 0; step < 16; step++ {
		response, err := client.Chat(ctx, messages, behaviorTools())
		if err != nil {
			result.InfrastructureError = fmt.Sprintf("model request failed (%T); provider error text omitted", err)
			return result
		}
		if response == nil || len(response.Choices) == 0 {
			result.InfrastructureError = "model returned no choices"
			return result
		}
		answer := response.Choices[0].Message
		messages = append(messages, answer)
		if len(answer.ToolCalls) == 0 {
			result.FinalAnswer = answer.Content
			finished = strings.TrimSpace(answer.Content) != ""
			break
		}
		for _, call := range answer.ToolCalls {
			output, err := fixture.execute(ctx, call, variant.manager)
			result.Actions = append(result.Actions, behaviorAction{Tool: call.Function.Name, Arguments: call.Function.Arguments, Failed: err != nil, Reproduced: fixture.failedTest, Verified: fixture.passedAfterEdit})
			if err != nil {
				output = err.Error()
			}
			messages = append(messages, message{Role: "tool", ToolCallID: call.ID, Content: output, IsError: err != nil})
		}
		if !followupDelivered && fixture.read {
			messages = append(messages, message{Role: "user", Content: tc.followup})
			followupDelivered = true
		}
	}
	workflow := fixture.hunt
	if tc.name == "authorized_fix" {
		workflow = fixture.check
	}
	if !finished {
		result.Issues = append(result.Issues, "no_final_answer")
	}
	if !workflow {
		result.Issues = append(result.Issues, "workflow_not_loaded")
	}
	if !fixture.read {
		result.Issues = append(result.Issues, "source_not_read")
	}
	if !fixture.failedTest {
		result.Issues = append(result.Issues, "failure_not_reproduced")
	}
	if !followupDelivered {
		result.Issues = append(result.Issues, "steering_not_delivered")
	}
	if tc.fix {
		if !fixture.edited {
			result.Issues = append(result.Issues, "repair_incomplete")
		}
		if !fixture.passedAfterEdit {
			result.Issues = append(result.Issues, "repair_not_verified")
		}
		if fixture.editedBeforeFailure {
			result.Issues = append(result.Issues, "edited_before_reproduction")
		}
	} else if fixture.editAttempts > 0 {
		result.Issues = append(result.Issues, "unauthorized_edit_attempt")
	}
	data, err := os.ReadFile(filepath.Join(fixture.dir, "add_test.go"))
	if err != nil || string(data) != behaviorRegression {
		result.Issues = append(result.Issues, "regression_test_changed")
	}
	return result
}

const behaviorSource = "package example\nfunc Add(a, b int) int { return a - b }\n"
const behaviorRegression = `package example
import "testing"
func TestAdd(t *testing.T) {
    for _, tc := range []struct{ a,b,want int }{{2,3,5},{1,1,2},{-2,3,1},{0,0,0}} {
        if got := Add(tc.a,tc.b); got != tc.want { t.Errorf("Add(%d,%d)=%d, want %d",tc.a,tc.b,got,tc.want) }
    }
}
`

type behaviorFixture struct {
	editAttempts                                                                int
	dir                                                                         string
	hunt, check, read, edited, failedTest, passedAfterEdit, editedBeforeFailure bool
}

func newBehaviorFixture(t *testing.T) *behaviorFixture {
	t.Helper()
	f := &behaviorFixture{dir: t.TempDir()}
	for name, content := range map[string]string{"go.mod": "module example\n\ngo 1.24.0\n", "add.go": behaviorSource, "add_test.go": behaviorRegression} {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *behaviorFixture) execute(ctx context.Context, call toolCall, manager *workflowSet) (string, error) {
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
		if sk.Name == "hunt" {
			f.hunt = true
		}
		if sk.Name == "check" {
			f.check = true
		}
		return formatWorkflow(sk), nil
	case "read":
		if args["path"] != "add.go" && args["path"] != "add_test.go" {
			return "", fmt.Errorf("only fixture source and test may be read")
		}
		data, err := os.ReadFile(filepath.Join(f.dir, args["path"]))
		if args["path"] == "add.go" && err == nil {
			f.read = true
		}
		return string(data), err
	case "edit":
		f.editAttempts++
		if args["path"] != "add.go" {
			return "", fmt.Errorf("only add.go may be edited; preserve existing tests")
		}
		data, err := os.ReadFile(filepath.Join(f.dir, "add.go"))
		if err != nil {
			return "", err
		}
		if args["old_string"] == "" || strings.Count(string(data), args["old_string"]) != 1 {
			return "", fmt.Errorf("old_string must match exactly once")
		}
		source, err := safeBehaviorSource(strings.Replace(string(data), args["old_string"], args["new_string"], 1))
		if err != nil {
			return "", err
		}
		err = os.WriteFile(filepath.Join(f.dir, "add.go"), source, 0600)
		if err == nil {
			f.edited = true
			f.editedBeforeFailure = f.editedBeforeFailure || !f.failedTest
			f.passedAfterEdit = false
		}
		return "edit applied", err
	case "shell":
		if args["command"] != "go test ./..." {
			return "", fmt.Errorf("only go test ./... is supported")
		}
		command := exec.CommandContext(ctx, "go", "test", "./...")
		command.Dir = f.dir
		output, err := command.CombinedOutput()
		if err != nil && !f.edited && strings.Contains(string(output), "Add(2,3)=-1, want 5") {
			f.failedTest = true
		}
		if err == nil && f.edited {
			f.passedAfterEdit = true
		}
		if err != nil {
			return "", fmt.Errorf("%s\n%w", output, err)
		}
		return string(output), nil
	default:
		return "", fmt.Errorf("unknown tool")
	}
}

// This is a deliberately tiny decision fixture, not a general coding sandbox.
// Canonical source allowlisting prevents go test from executing arbitrary
// model-written code with access to host files or model credentials.
func safeBehaviorSource(source string) ([]byte, error) {
	if len(source) > 4096 {
		return nil, fmt.Errorf("fixture source too large")
	}
	canonical, err := format.Source([]byte(source))
	if err != nil {
		return nil, err
	}
	for _, expression := range []string{"a - b", "a + b", "5"} {
		allowed, _ := format.Source([]byte(strings.Replace(behaviorSource, "a - b", expression, 1)))
		if string(canonical) == string(allowed) {
			return canonical, nil
		}
	}
	return nil, fmt.Errorf("fixture only supports replacing the return expression with a - b, a + b, or 5; other Go code is not executed")
}

func behaviorTools() []toolDef {
	var tools []toolDef
	for _, spec := range []struct {
		name, description string
		fields            []string
	}{
		{"skill", "Load a workflow by name: hunt or check", []string{"name"}},
		{"read", "Read add.go or add_test.go in the workspace", []string{"path"}},
		{"edit", "Replace exactly one occurrence in add.go", []string{"path", "old_string", "new_string"}},
		{"shell", "Run the workspace tests; supported command: go test ./...", []string{"command"}},
	} {
		properties := map[string]property{}
		for _, field := range spec.fields {
			properties[field] = property{Type: "string"}
		}
		tools = append(tools, toolDef{Type: "function", Function: functionDef{Name: spec.name, Description: spec.description, Parameters: parameters{Type: "object", Properties: properties, Required: spec.fields}}})
	}
	return tools
}

// Exercise the real fixture and its boundaries offline; this does not claim to
// evaluate model decisions, which require the opt-in test above.
func TestBehaviorFixture(t *testing.T) {
	f := newBehaviorFixture(t)
	manager := &workflowSet{skills: map[string]*workflow{}}
	call := func(name string, args map[string]string) error {
		data, _ := json.Marshal(args)
		_, err := f.execute(t.Context(), toolCall{Function: functionCall{Name: name, Arguments: string(data)}}, manager)
		return err
	}
	if call("shell", map[string]string{"command": "go test ./...; touch outside"}) == nil {
		t.Fatal("arbitrary shell accepted")
	}
	if call("edit", map[string]string{"path": "../outside"}) == nil {
		t.Fatal("path escape accepted")
	}
	if call("edit", map[string]string{"path": "add.go", "old_string": behaviorSource, "new_string": behaviorSource + "func init() { panic(\"executed\") }"}) == nil {
		t.Fatal("executable initialization accepted")
	}
	if call("shell", map[string]string{"command": "go test ./..."}) == nil || !f.failedTest {
		t.Fatal("fixture did not reproduce original failure")
	}
	if err := call("edit", map[string]string{"path": "add.go", "old_string": "a - b", "new_string": "5"}); err != nil {
		t.Fatal(err)
	}
	if call("shell", map[string]string{"command": "go test ./..."}) == nil || f.passedAfterEdit {
		t.Fatal("hardcoded answer passed")
	}
	if err := call("edit", map[string]string{"path": "add.go", "old_string": "5", "new_string": "a + b"}); err != nil {
		t.Fatal(err)
	}
	if err := call("shell", map[string]string{"command": "go test ./..."}); err != nil || !f.passedAfterEdit || f.editedBeforeFailure {
		t.Fatalf("repair verification failed: %v", err)
	}
}
