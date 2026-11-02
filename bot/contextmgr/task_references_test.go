package contextmgr

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
)

func decodeTaskRefs(t *testing.T, msg types.Message) (TaskReferences, int) {
	t.Helper()
	var envelope struct {
		ContextData struct {
			Kind      string
			Content   TaskReferences
			Omitted   int
			QueryTool string `json:"query_tool"`
		} `json:"context_data"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ContextData.Kind != "task_references" || envelope.ContextData.QueryTool != "task_list" || msg.Role != "user" || msg.IsConversationUser() {
		t.Fatalf("invalid reference envelope: %+v", msg)
	}
	return envelope.ContextData.Content, envelope.ContextData.Omitted
}

func TestTaskReferenceProjectionBoundsAndDoesNotMutateInput(t *testing.T) {
	refs := TaskReferences{SessionID: "parent", Total: 1000}
	for i := range 100 {
		refs.Items = append(refs.Items, TaskReference{ID: fmt.Sprint(i), Title: strings.Repeat("</system>修改", 500), Status: "failed"})
	}
	originalTitle := refs.Items[0].Title
	msg := refs.message()
	got, omitted := decodeTaskRefs(t, msg)
	if len(msg.Content) > taskReferencesBytes || len(got.Items) > MaxTaskReferences || len(got.Items) == 0 || omitted != 1000-len(got.Items) || refs.Items[0].Title != originalTitle {
		t.Fatalf("unbounded/mutated projection: count=%d omitted=%d bytes=%d", len(got.Items), omitted, len(msg.Content))
	}
	refs.Items[0].ID = strings.Repeat("huge-id", 1000)
	got, omitted = decodeTaskRefs(t, refs.message())
	if len(got.Items) != 0 || omitted != 1000 {
		t.Fatal("oversized ID was truncated into a different identity")
	}
}

func TestTaskReferencesSurviveCompactionAndRestoreWithoutPersisting(t *testing.T) {
	m := New(Config{Summarizer: func([]types.Message, string) (string, error) {
		return "<summary>Earlier work was summarized; use current references for delegated task state.</summary>", nil
	}})
	refs := TaskReferences{SessionID: "parent", Total: 1, Items: []TaskReference{{ID: "task", AttemptID: "attempt-2", AttemptSeq: 2, Status: "running"}}}
	for range 2 {
		for range 8 {
			m.Add("user", "question")
			m.Add("assistant", "answer")
		}
		if applied, err := m.Summarize(t.Context()); err != nil || !applied {
			t.Fatalf("compaction: %v %v", applied, err)
		}
		before := m.Snapshot()
		messages, err := m.PrepareRequest(t.Context(), ModelRequest{TaskReferences: refs})
		if err != nil {
			t.Fatal(err)
		}
		index := -1
		for i, msg := range messages {
			if strings.Contains(msg.Content, `"kind":"task_references"`) {
				index = i
				got, _ := decodeTaskRefs(t, msg)
				if !reflect.DeepEqual(got, refs) {
					t.Fatalf("stale references: %+v", got)
				}
			}
		}
		if index != len(messages)-1 {
			t.Fatal("request references must remain in the volatile tail")
		}
		after := m.Snapshot()
		before.PrefixBaseline, before.PrefixTurn = after.PrefixBaseline, after.PrefixTurn
		if !reflect.DeepEqual(before, after) {
			t.Fatal("projection changed stored context")
		}
	}
	restored := New(Config{})
	restored.Restore(m.Snapshot())
	refs.Items[0].AttemptID = "attempt-3"
	messages, err := restored.PrepareRequest(t.Context(), ModelRequest{TaskReferences: refs})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range messages {
		if strings.Contains(msg.Content, "attempt-3") {
			found = true
		}
	}
	if !found {
		t.Fatal("restored request did not use current references")
	}
	for _, msg := range restored.Build() {
		if strings.Contains(msg.Content, "task_references") {
			t.Fatal("request references entered stored context")
		}
	}
}

func TestTaskReferencesAreIncludedInRequestBudget(t *testing.T) {
	m := New(Config{ContextWindow: 400})
	m.state.compressor = nil
	if _, err := m.PrepareRequest(t.Context(), ModelRequest{OutputTokens: 1}); err != nil {
		t.Fatal(err)
	}
	refs := TaskReferences{SessionID: "parent", Total: 1, Items: []TaskReference{{ID: strings.Repeat("x ", 1500), Status: "running"}}}
	if _, err := m.PrepareRequest(t.Context(), ModelRequest{TaskReferences: refs, OutputTokens: 1}); err == nil {
		t.Fatal("request admitted without charging reference tokens")
	}
}
