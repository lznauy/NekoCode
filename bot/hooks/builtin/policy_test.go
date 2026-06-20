package builtin

import (
	"strings"
	"testing"
)

type testState struct {
	ints map[string]int64
	strs map[string]string
	tool string
	args map[string]any
}

func newState() *testState {
	return &testState{ints: make(map[string]int64), strs: make(map[string]string)}
}

func (s *testState) Get(key string) int64                     { return s.ints[key] }
func (s *testState) Set(key string, value int64)              { s.ints[key] = value }
func (s *testState) Flag(key string) bool                     { return s.ints[key] == 1 }
func (s *testState) GetStr(key string) string                 { return s.strs[key] }
func (s *testState) ToolName() string                         { return s.tool }
func (s *testState) ToolArgs() map[string]any                 { return s.args }
func (s *testState) SetStr(key, value string)                 { s.strs[key] = value }
func (s *testState) SetTool(name string, args map[string]any) { s.tool, s.args = name, args }

func TestQuotaHook(t *testing.T) {
	hk := QuotaHook()
	s := newState()

	s.Set(StoreQuotaReads, 5)
	if r := hk.On(s); r != nil {
		t.Fatal("reads=5 should be silent")
	}
	s.Set(StoreQuotaReads, 2)
	if r := hk.On(s); r == nil || r.Hint == nil || r.Hint.Severity != "warning" {
		t.Fatalf("reads=2 result = %+v, want warning hint", r)
	}
	if r := hk.On(s); r != nil {
		t.Fatal("same quota warning should dedupe")
	}
	s.Set(StoreQuotaReads, 0)
	if r := hk.On(s); r == nil || r.Hint == nil || r.Hint.Severity != "critical" {
		t.Fatalf("reads=0 result = %+v, want critical hint", r)
	}
}

func TestVerificationHook(t *testing.T) {
	hk := VerificationHook()
	s := newState()

	s.Set(StoreHasTasks, 0)
	if r := hk.On(s); r != nil {
		t.Fatal("no tasks should be silent")
	}
	s.Set(StoreHasTasks, 1)
	s.Set(StoreTasksAllDone, 1)
	if r := hk.On(s); r != nil {
		t.Fatal("all tasks done should be silent")
	}
	s.Set(StoreTasksAllDone, 0)
	if r := hk.On(s); r == nil || r.BlockFinal == nil || !strings.Contains(r.BlockFinal.Reason, "未完成") {
		t.Fatalf("unfinished no-tool result = %+v, want block final", r)
	}
	if r := hk.On(s); r != nil {
		t.Fatal("verification warning should dedupe")
	}
}

func TestGarbledCircuitBreaker(t *testing.T) {
	hk := GarbledCircuitBreaker()
	s := newState()

	s.Set(StoreRespGarbled, 4)
	if r := hk.On(s); r != nil {
		t.Fatal("count=4 should not stop")
	}
	s.Set(StoreRespGarbled, 5)
	if r := hk.On(s); r == nil || r.Stop == nil || *r.Stop != StopFormatError {
		t.Fatalf("count=5 result = %+v, want format stop", r)
	}
}

func TestCompletionQualityHook(t *testing.T) {
	hk := CompletionQualityHook()
	s := newState()
	s.Set(StoreStepInputLen, 100)

	s.Set(StoreTasksAllDone, 1)
	s.Set(StoreHasTasks, 1)
	s.Set(StoreLedgerModified, 2)
	if r := hk.On(s); r == nil || r.BlockFinal == nil || !strings.Contains(r.BlockFinal.Reason, "未验证") {
		t.Fatalf("modified unverified result = %+v, want block final", r)
	}

	s2 := newState()
	s2.Set(StoreStepInputLen, 100)
	s2.Set(StoreTasksAllDone, 1)
	s2.Set(StoreHasTasks, 1)
	if r := hk.On(s2); r == nil || r.Hint == nil || !strings.Contains(r.Hint.Content, "没有文件修改") {
		t.Fatalf("no modification result = %+v, want info hint", r)
	}
}

func TestProgressStallHook(t *testing.T) {
	hk := ProgressStallHook()
	s := newState()
	s.SetStr(StoreStepInput, "test task")
	s.Set(StoreTurnToolCalls, 1)

	for i := 0; i < 7; i++ {
		if r := hk.On(s); r != nil {
			t.Fatalf("stall turn %d result = %+v, want silent", i+1, r)
		}
	}
	if r := hk.On(s); r == nil || r.Hint == nil || r.RequireTool == nil {
		t.Fatalf("8th stall result = %+v, want warning and required tool", r)
	}
}

func TestExplorationHooks(t *testing.T) {
	exhausted := ExplorationExhaustedHook()
	s := newState()
	s.SetStr(StoreStepInput, "test task")
	s.Set(StoreExploreCalls, 10)

	r := exhausted.On(s)
	if r == nil || r.Hint == nil || r.RequireTool == nil {
		t.Fatalf("exploration exhausted result = %+v, want hint and required tool", r)
	}
	if r.StatePatch == nil || r.StatePatch.Ints[PolicyExploreExhausted] != 1 {
		t.Fatalf("state patch = %+v, want explore exhausted policy", r.StatePatch)
	}

	guard := ExplorationGuardHook()
	s.Set(PolicyExploreExhausted, 1)
	s.SetTool("read", nil)
	if r := guard.On(s); r == nil || r.BlockTool == nil {
		t.Fatalf("read after exhaustion result = %+v, want block", r)
	}
	s.SetTool("edit", map[string]any{"path": "/tmp/test.txt"})
	if r := guard.On(s); r != nil {
		t.Fatalf("edit after exhaustion result = %+v, want allow", r)
	}
}

func TestExploreCascadeHook(t *testing.T) {
	hk := ExploreCascadeHook()
	s := newState()
	s.SetStr(StoreStepInput, "test task")

	s.Set(StoreToolResearcher, 3)
	if r := hk.On(s); r != nil {
		t.Fatal("3 researchers should be silent")
	}
	s.Set(StoreToolResearcher, 4)
	if r := hk.On(s); r == nil || r.Hint == nil || r.Hint.Type != "explore_cascade" {
		t.Fatalf("4 researchers result = %+v, want cascade hint", r)
	}
}
