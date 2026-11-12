package runtime

import (
	"context"
	"fmt"
	"nekocode/protocol"
	"testing"
	"time"
)

func TestAllTasksPaginationAndDeletedProjection(t *testing.T) {
	deleted := false
	services := backgroundServices(t.TempDir())
	services.Sessions.ListSessions = func() []SessionMeta { return []SessionMeta{{ID: "two"}, {ID: "one"}} }
	services.Sessions.Subagents = func(parent string) ([]SubagentSummary, error) {
		items := []SubagentSummary{}
		for i := range 3 {
			if deleted && parent == "one" && i == 0 {
				continue
			}
			items = append(items, SubagentSummary{ID: fmt.Sprintf("%s-%d", parent, i), SessionID: parent, CreatedAt: time.Unix(int64(i), 0)})
		}
		return items, nil
	}
	services.Sessions.DeleteSubagent = func(parent, id string) error {
		if parent != "one" || id != "one-0" {
			return fmt.Errorf("wrong identity")
		}
		deleted = true
		return nil
	}
	services.Sessions.SubagentDeleted = func(parent, id string) bool { return deleted && parent == "one" && id == "one-0" }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	first, err := rt.Subagents(SubagentFilter{AllSessions: true, Limit: 2})
	if err != nil || first.Total != 6 || len(first.Items) != 2 || first.Items[0].SessionID != "one" || !first.HasMore {
		t.Fatalf("%+v %v", first, err)
	}
	next, err := rt.Subagents(SubagentFilter{AllSessions: true, Offset: 2, Limit: 2})
	if err != nil || next.Items[0].ID != "one-2" || next.Items[1].ID != "two-0" {
		t.Fatalf("%+v %v", next, err)
	}
	rt.publishStepForSession("old-run", protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "one-0", SubAgentStatus: "running"}, "one")
	rt.publishStepForSession("old-run", protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "one-0", SubAgentStatus: "completed"}, "one")
	if err := rt.DeleteSubagent(t.Context(), "one", "one-0"); err != nil {
		t.Fatal(err)
	}
	page, err := rt.Subagents(SubagentFilter{SessionID: "one", Limit: 10})
	if err != nil || page.Total != 2 {
		t.Fatalf("deleted task resurrected: %+v %v", page, err)
	}
}
