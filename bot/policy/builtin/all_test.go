package builtin

import (
	"strings"
	"testing"

	"nekocode/bot/policy"
)

type testState struct {
	facts policy.Facts
	ints  map[string]int64
	strs  map[string]string
}

func newState() *testState {
	return &testState{ints: make(map[string]int64), strs: make(map[string]string)}
}

func (s *testState) Facts() policy.Facts             { return s.facts }
func (s *testState) Int(name string) int64           { return s.ints[name] }
func (s *testState) SetInt(name string, value int64) { s.ints[name] = value }
func (s *testState) String(name string) string       { return s.strs[name] }
func (s *testState) SetString(name, value string)    { s.strs[name] = value }

func TestAllReturnsOnlyDeterministicHooks(t *testing.T) {
	hooks := All()
	if len(hooks) != 2 {
		t.Fatalf("builtin hooks = %d, want 2", len(hooks))
	}
	want := map[string]bool{"read_before_write": true, "garbled_circuit_breaker": true}
	for _, hook := range hooks {
		if !want[hook.Name] {
			t.Fatalf("unexpected builtin hook %q", hook.Name)
		}
		if hook.DescribeTrigger == nil {
			t.Fatalf("hook %q has no trigger description", hook.Name)
		}
	}
}

func TestGarbledCircuitBreaker(t *testing.T) {
	hook := GarbledCircuitBreaker()
	state := newState()
	state.facts.Response.GarbledCount = 4
	if result := hook.On(state); result != nil {
		t.Fatal("count=4 should not stop")
	}
	state.facts.Response.GarbledCount = 5
	if result := hook.On(state); result == nil || result.Stop == nil || *result.Stop != policy.StopFormatError {
		t.Fatalf("count=5 result = %+v, want format stop", result)
	}
}

func TestGarbledCircuitBreakerHintsReformat(t *testing.T) {
	hook := GarbledCircuitBreaker()
	state := newState()
	state.facts.Response.GarbledCount = 1
	state.facts.Response.Intent = policy.FinalIntentFormatError
	result := hook.On(state)
	if result == nil || result.Hint == nil || result.Hint.Type == "" || result.Hint.Content == "" {
		t.Fatalf("format error should ask the model to redo the call: %+v", result)
	}
	if result.Stop != nil {
		t.Fatalf("a single format error must not stop the run: %+v", result)
	}

	state.facts.Response.Intent = policy.FinalIntentFinal
	if result := hook.On(state); result != nil {
		t.Fatalf("clean final answer should not be hinted: %+v", result)
	}
}

func TestReadBeforeWriteHook(t *testing.T) {
	hook := ReadBeforeWriteHook()
	state := newState()
	state.facts.Tool = policy.ToolFacts{Name: "edit", TargetPath: "main.go", TargetExists: true}

	if result := hook.On(state); result == nil || result.BlockTool == nil || !strings.Contains(result.BlockTool.Reason, "main.go") {
		t.Fatalf("unread edit result = %+v, want block", result)
	}
	state.facts.Tool.TargetWasRead = true
	if result := hook.On(state); result != nil {
		t.Fatalf("read target should pass, got %+v", result)
	}
}

func TestReadBeforeWriteRejectsLongAnchorAsReadEvidence(t *testing.T) {
	hook := ReadBeforeWriteHook()
	state := newState()
	state.facts.Tool = policy.ToolFacts{
		Name: "edit", TargetPath: "main.go", TargetExists: true,
		Args: map[string]any{"oldString": strings.Repeat("known text ", 40)},
	}
	if result := hook.On(state); result == nil || result.BlockTool == nil {
		t.Fatalf("long edit anchor bypassed read-before-write: %+v", result)
	}
}

func TestReadBeforeWriteTriggerHandlesMissingTarget(t *testing.T) {
	got := ReadBeforeWriteHook().DescribeTrigger(newState())
	if !strings.HasPrefix(got, "target=- ") {
		t.Fatalf("trigger = %q, want target=- for missing path", got)
	}
}
