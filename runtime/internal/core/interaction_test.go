package core

import (
	"encoding/json"
	"testing"
)

func TestApprovalCancellationIsInternalMetadata(t *testing.T) {
	var decision ApprovalDecision
	if err := json.Unmarshal([]byte(`{"allowed":false,"Cancelled":true,"cancelled":true}`), &decision); err != nil {
		t.Fatal(err)
	}
	if decision.Cancelled || decision.ConfirmReply().Cancelled {
		t.Fatal("client supplied internal cancellation metadata")
	}
	decision.Cancelled = true
	if !decision.ConfirmReply().Cancelled {
		t.Fatal("in-process cancellation lost")
	}
	data, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["Cancelled"] != nil || fields["cancelled"] != nil {
		t.Fatalf("internal metadata exposed: %s", data)
	}
}
