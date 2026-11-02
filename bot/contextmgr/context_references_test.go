package contextmgr

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/provider/types"
)

func TestArtifactReferencesBoundRequestAndKeepDroppedItemsReachable(t *testing.T) {
	page := refbridge.Page{SessionID: "parent", Before: 1, Scanned: 20, Path: "PRIVATE-PATH", Status: "available"}
	for i := 20; i > 0; i-- {
		page.Items = append(page.Items, refbridge.Reference{ID: fmt.Sprintf("artifact.%d.%s", i, strings.Repeat("a", 64)), Sequence: i})
	}
	original := append([]refbridge.Reference(nil), page.Items...)
	msg := contextReferencesMessage(page)
	var envelope struct {
		ContextData struct {
			Content refbridge.Page
			Omitted int
		} `json:"context_data"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &envelope); err != nil {
		t.Fatal(err)
	}
	got := envelope.ContextData.Content
	if len(msg.Content) > contextReferencesBytes || len(got.Items) != 16 || got.Path != "" || got.Status != "" || strings.Contains(msg.Content, "PRIVATE-PATH") {
		t.Fatalf("unsafe projection: %s", msg.Content)
	}
	if got.Before != 5 || !got.HasMore || envelope.ContextData.Omitted != 4 {
		t.Fatalf("truncated page skipped old refs: %+v", got)
	}
	if !reflect.DeepEqual(original, page.Items) || page.Before != 1 || page.Path != "PRIVATE-PATH" {
		t.Fatal("projection mutated source")
	}
	shifted := page
	shifted.Before, shifted.Scanned, shifted.HasMore = 99, 100, true
	if contextReferencesMessage(shifted).Content != msg.Content {
		t.Fatal("moving source window invalidated unchanged reference prefix")
	}
	if msg.Role != "user" || msg.IsConversationUser() {
		t.Fatal("reference projection became instruction/user turn")
	}
}

func TestArtifactReferencesSurviveTwoCompactionsAsRequestData(t *testing.T) {
	m := New(Config{Summarizer: func([]types.Message, string) (string, error) {
		return "<summary>Older messages were summarized; follow current user requests.</summary>", nil
	}})
	refs := refbridge.Page{SessionID: "parent", Items: []refbridge.Reference{{ID: "saved-artifact-id", Sequence: 2}}}
	for range 2 {
		for range 8 {
			m.Add("user", "question")
			m.Add("assistant", "answer")
		}
		if applied, err := m.Summarize(t.Context()); err != nil || !applied {
			t.Fatalf("compaction=%v %v", applied, err)
		}
		before := m.Snapshot()
		messages, err := m.PrepareRequest(t.Context(), ModelRequest{ContextReferences: refs})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for i, msg := range messages {
			if strings.Contains(msg.Content, `"kind":"context_references"`) {
				found = true
				if i != len(messages)-1 || !strings.Contains(msg.Content, "saved-artifact-id") {
					t.Fatal("references missing from volatile tail")
				}
			}
		}
		if !found {
			t.Fatal("request omitted references after compaction")
		}
		after := m.Snapshot()
		before.PrefixBaseline, before.PrefixTurn = after.PrefixBaseline, after.PrefixTurn
		if !reflect.DeepEqual(before, after) {
			t.Fatal("artifact references entered stored context")
		}
	}
	restored := New(Config{})
	restored.Restore(m.Snapshot())
	for _, msg := range restored.Build() {
		if strings.Contains(msg.Content, "saved-artifact-id") {
			t.Fatal("references persisted as conversation")
		}
	}
}

func TestArtifactReferencesParticipateInFinalRequestBudget(t *testing.T) {
	m := New(Config{ContextWindow: 400})
	m.state.compressor = nil
	if _, err := m.PrepareRequest(t.Context(), ModelRequest{OutputTokens: 1}); err != nil {
		t.Fatal(err)
	}
	refs := refbridge.Page{SessionID: "parent", Items: []refbridge.Reference{{ID: strings.Repeat("x ", 1500), Sequence: 2}}}
	if _, err := m.PrepareRequest(t.Context(), ModelRequest{OutputTokens: 1, ContextReferences: refs}); err == nil {
		t.Fatal("request omitted reference budget")
	}
}

func TestSkillAndArtifactReferencesShareExistingRequestBudget(t *testing.T) {
	page := refbridge.Page{SessionID: "parent", HasMore: true}
	for i := 20; i > 0; i-- {
		kind := "artifact"
		name := ""
		hash := ""
		if i%2 == 0 {
			kind = "skill"
			name = strings.Repeat("skill-name", 8)
			hash = types.SkillContentHash("workflow")
		}
		page.Items = append(page.Items, refbridge.Reference{Kind: kind, ID: fmt.Sprintf("%s.%d.%s", kind, i, strings.Repeat("a", 64)), Sequence: i, Name: name, ContentHash: hash})
	}
	msg := contextReferencesMessage(page)
	var envelope struct {
		ContextData struct {
			Content refbridge.Page
			Omitted int
		} `json:"context_data"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &envelope); err != nil {
		t.Fatal(err)
	}
	skills, artifacts := 0, 0
	for _, ref := range envelope.ContextData.Content.Items {
		if ref.Kind == "skill" {
			skills++
		} else {
			artifacts++
		}
	}
	if skills == 0 || skills > 8 || artifacts > 16 || len(msg.Content) > 4096 || envelope.ContextData.Omitted != 20-skills-artifacts {
		t.Fatalf("unbounded shared projection: skills=%d artifacts=%d bytes=%d", skills, artifacts, len(msg.Content))
	}
	retained := envelope.ContextData.Content.Items
	if envelope.ContextData.Content.Before != retained[len(retained)-1].Sequence || !envelope.ContextData.Content.HasMore {
		t.Fatal("shared cap skipped omitted sources")
	}
	task := TaskReferences{SessionID: "parent", Total: 1, Items: []TaskReference{{ID: strings.Repeat("task", 500), Status: "running"}}}.message()
	if len(task.Content)+len(msg.Content) > 8192 {
		t.Fatal("combined reference budget grew")
	}
}
