package core

import (
	"fmt"
	"reflect"
	"testing"

	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestReferenceHistoryMergesEveryDurablePendingBoundary(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "one", Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "one", ToolOutput: protocol.ToolOutputArtifact{Path: "/actor/one"}},
		{Role: "user", Content: "another question"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "two", Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "two", ToolOutput: protocol.ToolOutputArtifact{Path: "/actor/two"}},
		{Role: "user", Content: "ordinary"},
		{Role: "user", Source: types.MessageSourceSkillWorkflow, Content: "workflow", SkillDelivery: types.SkillDelivery{Schema: 1, Name: "review", Source: "selected", State: types.SkillReady, ContentHash: types.SkillContentHash("workflow"), OriginSequence: 8}},
		{Role: "assistant", Content: "answer"},
	}
	for split := 0; split <= len(history); split++ {
		t.Run(fmt.Sprint(split), func(t *testing.T) {
			b := newPersistTestBot(t)
			if split > 0 {
				b.ctxMgr.Restore(contextmgr.ManagerSnapshot{Messages: history[:split], Transcript: history[:split]})
				if err := b.saveSession(); err != nil {
					t.Fatal(err)
				}
			}
			b.ctxMgr.Restore(contextmgr.ManagerSnapshot{Messages: history, Transcript: history[split:], TranscriptBase: split, TranscriptLazy: true})
			for before := 0; before <= len(history)+1; before++ {
				for _, limit := range []int{1, 2, 20} {
					page, err := b.contextReferences(t.Context(), b.CurrentSessionID(), refbridge.Query{Before: before, Limit: limit})
					if err != nil {
						t.Fatal(err)
					}
					var expected []int
					for _, seq := range []int{8, 6, 3} {
						if before == 0 || seq < before {
							expected = append(expected, seq)
						}
					}
					more := len(expected) > limit
					expected = expected[:min(len(expected), limit)]
					var got []int
					for _, ref := range page.Items {
						got = append(got, ref.Sequence)
					}
					if !reflect.DeepEqual(got, expected) || page.HasMore != more || page.Scanned != len(expected) {
						t.Fatalf("before=%d limit=%d: %+v want %v more=%v", before, limit, page, expected, more)
					}
				}
			}
		})
	}
}

func TestColdReferenceProjectionRetainsOldOutputAfterThousandsOfMessages(t *testing.T) {
	b := newPersistTestBot(t)
	addReferenceOutput(t, b, "old-output", false)
	scope := b.CurrentSessionID()
	first := b.modelContextReferences(t.Context(), scope)
	if len(first.Items) != 1 {
		t.Fatal("missing initial output reference")
	}
	for range 10000 {
		b.ctxMgr.Add("user", "unrelated ordinary history")
	}
	snap := b.ctxMgr.Snapshot()
	snap.Messages = snap.Messages[len(snap.Messages)-1:]
	b.ctxMgr.Restore(snap)
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	if err := b.ResumeSession(scope); err != nil {
		t.Fatal(err)
	}
	base, tail, lazy := b.ctxMgr.TranscriptTail()
	if base < 10000 || len(tail) != 0 || !lazy {
		t.Fatal("cold restore eagerly retained transcript")
	}
	page := b.modelContextReferences(t.Context(), scope)
	if page.Unavailable || len(page.Items) != 1 || page.Items[0].ID != first.Items[0].ID || page.Scanned != 1 || page.HasMore {
		t.Fatalf("old output displaced after cold restore: %+v", page)
	}
}
