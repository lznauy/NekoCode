package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"nekocode/protocol"
)

func TestNewerPersistedAttemptWinsOverOldRuntimeTerminal(t *testing.T) {
	for _, status := range []string{"running", "interrupted"} {
		bot := &testBot{}
		services := testBotServices(bot)
		services.Sessions.Subagents = func(string) ([]SubagentSummary, error) {
			return []SubagentSummary{{ID: "saved", Status: status, RunCount: 2, UpdatedAt: time.Now().Add(time.Second)}}, nil
		}
		rt := New(bot, services)
		bot.run = func(_ string, host RunHost) (string, error) {
			host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "saved"})
			host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "saved", SubAgentStatus: "completed"})
			return "done", nil
		}
		id, err := rt.StartRun(context.Background(), Input{Text: "work"})
		if err != nil {
			t.Fatal(err)
		}
		waitForRun(t, rt, id)
		page, err := rt.Subagents(SubagentFilter{Limit: 10})
		rt.Close()
		if err != nil || len(page.Items) != 1 || page.Items[0].Status != status {
			t.Fatalf("new %s attempt masked by cached completion: %+v err=%v", status, page, err)
		}
	}
}

func TestAttemptIdentityArbitratesInsteadOfTimestamps(t *testing.T) {
	bot := &testBot{}
	services := testBotServices(bot)
	services.Sessions.Subagents = func(string) ([]SubagentSummary, error) {
		return []SubagentSummary{{ID: "saved", Status: "running", RunCount: 2, UpdatedAt: time.Unix(1, 0), LatestAttempt: &SubagentAttemptSummary{ID: "new", Seq: 2, Status: "running"}}}, nil
	}
	rt := New(bot, services)
	defer rt.Close()
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "saved", SubAgentAttemptID: "old", SubAgentAttemptSeq: 1})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "saved", SubAgentAttemptID: "new", SubAgentAttemptSeq: 2})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "saved", SubAgentAttemptID: "old", SubAgentAttemptSeq: 1, SubAgentStatus: "failed"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "saved", SubAgentAttemptID: "new", SubAgentAttemptSeq: 2, SubAgentStatus: "completed"})
		return "done", nil
	}
	id, err := rt.StartRun(context.Background(), Input{Text: "work"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, id)
	page, err := rt.Subagents(SubagentFilter{Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != "completed" || page.Items[0].LatestAttempt.ID != "new" {
		t.Fatalf("late old attempt polluted new attempt: %+v err=%v", page, err)
	}
}

func TestSameAttemptCommittedTerminalCannotBeDowngraded(t *testing.T) {
	bot := &testBot{}
	services := testBotServices(bot)
	services.Sessions.Subagents = func(string) ([]SubagentSummary, error) {
		return []SubagentSummary{{ID: "saved", Status: "completed", Result: "saved result", RunCount: 1, LatestAttempt: &SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "completed", Result: "saved result"}}}, nil
	}
	rt := New(bot, services)
	defer rt.Close()
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "saved", SubAgentAttemptID: "attempt", SubAgentAttemptSeq: 1})
		return "done", nil
	}
	id, err := rt.StartRun(context.Background(), Input{Text: "work"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, id)
	page, err := rt.Subagents(SubagentFilter{Limit: 10})
	if err != nil || page.Items[0].Status != "completed" || page.Items[0].LatestAttempt.Status != "completed" {
		t.Fatalf("stale live start downgraded committed result: %+v err=%v", page, err)
	}
}

func TestResumedTaskLatestLifecycleWinsAcrossParentRuns(t *testing.T) {
	bot := &testBot{}
	services := testBotServices(bot)
	services.Sessions.Subagents = func(string) ([]SubagentSummary, error) {
		return []SubagentSummary{{ID: "saved", SessionID: "parent", Status: "completed"}}, nil
	}
	rt := New(bot, services)
	defer rt.Close()
	for _, status := range []string{"failed", "completed"} {
		bot.run = func(_ string, host RunHost) (string, error) {
			host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "saved"})
			host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "saved", SubAgentStatus: status})
			return "done", nil
		}
		id, err := rt.StartRun(context.Background(), Input{Text: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		waitForRun(t, rt, id)
	}
	page, err := rt.Subagents(SubagentFilter{Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != "completed" {
		t.Fatalf("old attempt masked resume: %+v err=%v", page, err)
	}
}

func TestNewerSavedCompletionWinsOverCachedFailure(t *testing.T) {
	bot := &testBot{}
	services := testBotServices(bot)
	services.Sessions.Subagents = func(string) ([]SubagentSummary, error) {
		return []SubagentSummary{{ID: "saved", Status: "completed", UpdatedAt: time.Now().Add(time.Hour)}}, nil
	}
	rt := New(bot, services)
	defer rt.Close()
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "saved"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "saved", SubAgentStatus: "failed"})
		return "done", nil
	}
	id, err := rt.StartRun(context.Background(), Input{Text: "work"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, id)
	page, err := rt.Subagents(SubagentFilter{Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != "completed" {
		t.Fatalf("newer saved completion masked: %+v err=%v", page, err)
	}
}

func TestSubagentListMergesLifecycleWithoutCrossingSessions(t *testing.T) {
	bot := &testBot{}
	services := testBotServices(bot)
	services.Sessions = SessionsServices{
		CurrentSessionID: func() string { return "parent" }, ListSessions: func() []SessionMeta { return nil }, SessionMessages: func() []DisplayMessage { return nil },
		Subagents: func(id string) ([]SubagentSummary, error) {
			return []SubagentSummary{{ID: "saved", SessionID: id, Status: "running", Profile: "explore"}}, nil
		},
	}
	rt := New(bot, services)
	defer rt.Close()
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "saved", SubAgentProfile: "explore"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentUpdate, SubAgentID: "saved", SubAgentPhase: "Running read", SubAgentStatus: "running"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "saved", SubAgentStatus: "completed"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "live", SubAgentProfile: "coder", SubAgentPrompt: strings.Repeat("任务", 2000)})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "live", SubAgentStatus: "failed"})
		return "done", nil
	}
	id, err := rt.StartRun(context.Background(), Input{Text: "work"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, id)
	page, err := rt.Subagents(SubagentFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("lost live or saved row: %+v", page)
	}
	for _, item := range page.Items {
		if item.ID == "saved" && item.Status != "completed" {
			t.Fatalf("stale disk status overrode completion: %+v", item)
		}
		if item.ID == "live" && (item.Status != "failed" || len(item.Prompt) > 1024) {
			t.Fatalf("invalid live summary: %+v", item)
		}
	}
	other, err := rt.Subagents(SubagentFilter{SessionID: "other", Limit: 100})
	if err != nil || other.Total != 1 {
		t.Fatalf("cross-session live leak: %+v %v", other, err)
	}
}

func TestSubagentListFiltersAndPaginatesHundredTasks(t *testing.T) {
	services := ServiceGroups{Sessions: SessionsServices{Subagents: func(parent string) ([]SubagentSummary, error) {
		items := make([]SubagentSummary, 100)
		for i := range items {
			items[i] = SubagentSummary{ID: fmt.Sprintf("agent-%03d", i), SessionID: parent, Profile: "explore", Prompt: "inspect module", Status: "completed", CreatedAt: time.Unix(int64(i), 0)}
		}
		return items, nil
	}}}
	rt := New(&testBot{}, services)
	defer rt.Close()
	page, err := rt.Subagents(SubagentFilter{SessionID: "parent", Offset: 20, Limit: 10, Status: "completed"})
	if err != nil || page.Total != 100 || len(page.Items) != 10 || page.Items[0].ID != "agent-020" || page.NextOffset != 30 || !page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	page, err = rt.Subagents(SubagentFilter{SessionID: "parent", Query: "AGENT-099", Limit: 10})
	if err != nil || page.Total != 1 || page.Items[0].ID != "agent-099" {
		t.Fatalf("search=%+v err=%v", page, err)
	}
}

func TestSubagentReadersValidateBeforeCallingService(t *testing.T) {
	called := false
	rt := New(&testBot{}, ServiceGroups{Sessions: SessionsServices{
		Subagents: func(string) ([]SubagentSummary, error) { called = true; return nil, nil },
		SubagentHistory: func(parent, id string, before, limit int) (SubagentHistoryPage, error) {
			called = true
			return SubagentHistoryPage{SessionID: parent, SubagentID: id}, nil
		},
	}})
	for _, filter := range []SubagentFilter{{Limit: 0}, {Offset: -1, Limit: 1}, {Limit: 1001}} {
		if _, err := rt.Subagents(filter); err == nil {
			t.Fatal("accepted invalid list arguments")
		}
	}
	for _, args := range [][2]int{{-1, 1}, {0, 0}, {0, 1001}} {
		if _, err := rt.SubagentHistory("parent", "child", args[0], args[1]); err == nil {
			t.Fatal("accepted invalid history arguments")
		}
	}
	if called {
		t.Fatal("invalid input reached services")
	}
	page, err := rt.SubagentHistory("parent", "child", 0, 100)
	if err != nil || page.SessionID != "parent" {
		t.Fatalf("history=%+v err=%v", page, err)
	}
	rt.Close()
	if _, err := rt.Subagents(SubagentFilter{Limit: 1}); err == nil {
		t.Fatal("closed runtime accepted observation")
	}
	if _, err := rt.SubagentHistory("parent", "child", 0, 1); err == nil {
		t.Fatal("closed runtime accepted history")
	}
}

func TestSubagentObservationRequiresConfiguredServices(t *testing.T) {
	rt := New(&testBot{}, ServiceGroups{})
	defer rt.Close()
	if rt.SubagentObservationAvailable() {
		t.Fatal("optional methods advertised observation without services")
	}
	if _, err := rt.SubagentToolOutput("parent", "child", 1, 0); err == nil {
		t.Fatal("missing output service accepted request")
	}
}

func TestSubagentOutputValidatesAndStopsAfterClose(t *testing.T) {
	calls := 0
	rt := New(&testBot{}, ServiceGroups{Sessions: SessionsServices{
		SubagentToolOutput: func(parent, id string, sequence int, offset int64) (SubagentOutputPage, error) {
			calls++
			return SubagentOutputPage{Content: "stored output"}, nil
		},
	}})
	for _, args := range []struct {
		id       string
		sequence int
		offset   int64
	}{{"", 1, 0}, {"child", 0, 0}, {"child", 1, -1}} {
		if _, err := rt.SubagentToolOutput("parent", args.id, args.sequence, args.offset); err == nil {
			t.Fatal("invalid output cursor accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid input reached output service")
	}
	page, err := rt.SubagentToolOutput("parent", "child", 1, 0)
	if err != nil || page.Content != "stored output" || calls != 1 {
		t.Fatalf("output=%+v calls=%d err=%v", page, calls, err)
	}
	rt.Close()
	if _, err := rt.SubagentToolOutput("parent", "child", 1, 0); err == nil || calls != 1 {
		t.Fatal("closed runtime invoked output service")
	}
}
