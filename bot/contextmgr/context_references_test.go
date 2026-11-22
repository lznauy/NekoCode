package contextmgr

import "testing"

func TestEmptyTaskReferencesOnlyReportActionableState(t *testing.T) {
	if msg := (TaskReferences{SessionID: "s"}).message(); msg.Content != "" {
		t.Fatal(msg.Content)
	}
	for _, refs := range []TaskReferences{{SessionID: "s", Total: 1}, {SessionID: "s", Unavailable: true}} {
		if refs.message().Content == "" {
			t.Fatal("lost task status")
		}
	}
}
