package contextmgr

import (
	"errors"
	"nekocode/bot/provider/types"
	"reflect"
	"testing"
)

func TestCommittedCompactionExplainsNextCacheMiss(t *testing.T) {
	manager := New(Config{SystemPrompt: "stable", Summarizer: func([]types.Message, string) (string, error) {
		return "<summary>The original request, completed investigation and remaining tasks are retained.</summary>", nil
	}})
	for i := 0; i < 8; i++ {
		manager.Add("user", "question")
		manager.Add("assistant", "answer")
	}
	manager.BuildRequest(ModelRequest{})
	compacted, err := manager.Summarize(t.Context())
	if err != nil || !compacted {
		t.Fatalf("compact=%v err=%v", compacted, err)
	}
	manager.BeginModelTurn()
	manager.projectRequest(ModelRequest{}, false)
	manager.BuildRequest(ModelRequest{})
	want := []string{"history", "compaction"}
	if got := manager.PrefixDiagnostics().ChangedParts; !reflect.DeepEqual(got, want) {
		t.Fatalf("calllog diagnosis=%v want=%v", got, want)
	}
	manager.RecordModelUsage(types.StreamUsage{PromptTokens: 30, CacheHitTokens: 10, CacheMissTokens: 20, CacheUsageReported: true})
	if got := manager.Report().PrefixTurn.PeakMiss.Parts; !reflect.DeepEqual(got, want) {
		t.Fatalf("context report diagnosis=%v", got)
	}
	manager.Add("assistant", "new tail")
	manager.BuildRequest(ModelRequest{})
	if got := manager.PrefixDiagnostics().ChangedParts; !reflect.DeepEqual(got, []string{"tail/provider"}) {
		t.Fatalf("stale compaction attribution=%v", got)
	}
}

func TestFailedCompactionDoesNotClaimPrefixChange(t *testing.T) {
	manager := New(Config{Summarizer: func([]types.Message, string) (string, error) { return "", errors.New("unavailable") }})
	for i := 0; i < 8; i++ {
		manager.Add("user", "question")
		manager.Add("assistant", "answer")
	}
	manager.BuildRequest(ModelRequest{})
	if _, err := manager.Summarize(t.Context()); err == nil {
		t.Fatal("expected summary failure")
	}
	manager.BuildRequest(ModelRequest{})
	if got := manager.PrefixDiagnostics().ChangedParts; !reflect.DeepEqual(got, []string{"tail/provider"}) {
		t.Fatalf("failed compaction attributed=%v", got)
	}
}

func TestArchiveRewriteWithoutCompactionKeepsGenericAttribution(t *testing.T) {
	manager := New(Config{SystemPrompt: "stable"})
	manager.Add("user", "request")
	manager.BuildRequest(ModelRequest{})
	manager.state.ctx.Archive = "manual historical data update"
	manager.BuildRequest(ModelRequest{})
	if got := manager.PrefixDiagnostics().ChangedParts; !reflect.DeepEqual(got, []string{"history"}) {
		t.Fatalf("non-compaction rewrite misattributed: %v", got)
	}
}
