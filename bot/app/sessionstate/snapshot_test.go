package sessionstate

import (
	"reflect"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/llm/types"
	"nekocode/bot/session"
)

func TestApplyContextSnapshot(t *testing.T) {
	sess := &session.Snapshot{}
	snap := ctxmgr.ManagerSnapshot{
		SystemPrompt:    "sys",
		Skills:          "skills",
		Memory:          "mem",
		Archive:         "arch",
		Messages:        []types.Message{{Role: "user", Content: "hi"}},
		CompactBoundary: 3,
		Budget:          100,
	}
	ApplyContextSnapshot(sess, snap, 10, 20, map[string]bool{"b": true, "a": true, "skip": false})

	if sess.SystemPrompt != "sys" || sess.ContextWindow != 100 || sess.PromptTokens != 10 || sess.CompletionTokens != 20 {
		t.Fatalf("session fields not applied: %+v", sess)
	}
	if !reflect.DeepEqual(sess.LoadedSkills, []string{"a", "b"}) {
		t.Fatalf("loaded skills = %+v", sess.LoadedSkills)
	}
}

func TestManagerSnapshot(t *testing.T) {
	sess := &session.Snapshot{
		SystemPrompt:    "sys",
		Skills:          "skills",
		Memory:          "mem",
		Archive:         "arch",
		CompactBoundary: 2,
		ContextWindow:   50,
	}
	got := ManagerSnapshot(sess)
	if got.SystemPrompt != "sys" || got.Skills != "skills" || got.Budget != 50 || got.CompactBoundary != 2 {
		t.Fatalf("snapshot mismatch: %+v", got)
	}
}
