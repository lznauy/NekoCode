package contextmgr

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/protocol"
)

func TestEvidenceReferenceBudgetAndRecordCursor(t *testing.T) {
	page := refbridge.Page{SessionID: "session", HasMore: true}
	for n := 20; n > 0; n-- {
		page.Evidence = append(page.Evidence, refbridge.EvidenceItem{ID: fmt.Sprintf("evidence.%d.%064d", (n+1)/2, n), Sequence: (n + 1) / 2, Tool: "shell", Status: "success", Detail: &protocol.ToolEvidence{ReturnedHash: "SECRET"}})
	}
	msg := evidenceReferencesMessage(page, 4096)
	var decoded struct {
		Data struct {
			Content struct {
				Items []struct {
					Sequence int `json:"sequence"`
				}
				Before  int  `json:"before"`
				HasMore bool `json:"has_more"`
			} `json:"content"`
		} `json:"context_data"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &decoded); err != nil {
		t.Fatal(err)
	}
	items := decoded.Data.Content.Items
	if len(items) != 16 || len(msg.Content) > 4096 || strings.Contains(msg.Content, "SECRET") || strings.Contains(msg.Content, "detail") {
		t.Fatalf("invalid projection: %s", msg.Content)
	}
	if items[len(items)-1].Sequence != decoded.Data.Content.Before || !decoded.Data.Content.HasMore {
		t.Fatal("invalid cursor")
	}
	// Tight byte budgets must also retain both invocations of a source together.
	for budget := 256; budget < 4096; budget += 37 {
		bounded := evidenceReferencesMessage(page, budget)
		if len(bounded.Content) > budget {
			t.Fatal("budget exceeded")
		}
		if bounded.Content == "" {
			continue
		}
		if err := json.Unmarshal([]byte(bounded.Content), &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded.Data.Content.Items)%2 != 0 {
			t.Fatalf("split source at budget %d", budget)
		}
	}
	page.Before = 999
	page.Scanned = 123
	if evidenceReferencesMessage(page, 4096).Content != msg.Content {
		t.Fatal("moving scan metadata changed prefix")
	}
}

func TestReferenceViewsShareBudget(t *testing.T) {
	refs := refbridge.Page{SessionID: "s"}
	evidence := refbridge.Page{SessionID: "s"}
	for n := 0; n < 20; n++ {
		refs.Items = append(refs.Items, refbridge.Reference{Kind: "artifact", ID: strings.Repeat("a", 180), Sequence: 20 - n})
		evidence.Evidence = append(evidence.Evidence, refbridge.EvidenceItem{ID: strings.Repeat("b", 180), Sequence: 20 - n, Tool: "shell", Status: "success"})
	}
	messages := referenceMessages(refs, evidence)
	if len(messages) != 2 {
		t.Fatalf("want both views, got %d", len(messages))
	}
	size := 0
	for _, msg := range messages {
		size += len(msg.Content)
	}
	if size > contextReferencesBytes {
		t.Fatalf("combined references: %d", size)
	}
	empty := referenceMessages(refs, refbridge.Page{SessionID: "s"})
	if len(empty) != 1 || empty[0].Content != contextReferencesMessage(refs).Content {
		t.Fatal("empty evidence shrank existing view")
	}
}
