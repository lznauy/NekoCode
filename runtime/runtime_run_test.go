package runtime

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func TestCompactionStepHasDedicatedRunEvent(t *testing.T) {
	bot := &testBot{}
	bot.run = func(_ string, host RunHost) (string, error) {
		for _, status := range []protocol.CompactionStatus{protocol.CompactionStarted, protocol.CompactionDelta, protocol.CompactionCompleted} {
			host.Step(protocol.StepEvent{Action: protocol.StepActionCompaction, Compaction: &protocol.CompactionEvent{ID: "compact", Status: status, Summary: "summary"}})
		}
		return "answer", nil
	}
	rt := newTestRuntime(bot)
	// A live subscriber sees the full stream, deltas included.
	live, err := rt.events.Subscribe(context.Background(), core.EventFilter{Types: []core.EventType{core.EventCompaction}})
	if err != nil {
		t.Fatal(err)
	}
	id, err := rt.StartRun(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, id)
	var streamed []core.Event
	for range 3 {
		select {
		case ev := <-live:
			streamed = append(streamed, ev)
		case <-time.After(time.Second):
			t.Fatalf("subscriber missing compaction events: %+v", streamed)
		}
	}
	// History keeps only the durable events: deltas are excluded so a long
	// compaction cannot evict run boundary events from the replay window.
	events := rt.events.History(EventFilter{RunID: id, Types: []EventType{EventCompaction}})
	if len(events) != 2 {
		t.Fatalf("history should hold started+completed only: %+v", events)
	}
	for _, e := range events {
		p, ok := e.Payload.(CompactionPayload)
		if !ok {
			t.Fatalf("untyped payload: %+v", e)
		}
		if p.Status == protocol.CompactionDelta {
			t.Fatalf("delta must not persist to history: %+v", e)
		}
	}
	if text := rt.events.History(EventFilter{RunID: id, Types: []EventType{EventAssistantDelta}}); len(text) != 0 {
		t.Fatalf("summary leaked into response: %+v", text)
	}
}

func TestManagerCommandFinishesWithoutRunningAgent(t *testing.T) {
	bot := &testBot{}
	bot.command = func(string, RunHost) CommandResult {
		return CommandResult{Action: CommandHandled, Output: "handled"}
	}
	bot.run = func(string, RunHost) (string, error) {
		t.Fatal("agent should not run for handled command without skill continuation")
		return "", nil
	}
	rt := newTestRuntime(bot)

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "/help"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForRun(t, rt, runID)
	run, ok := rt.LookupRun(runID)
	if !ok {
		t.Fatal("run snapshot missing")
	}
	if run.Status != RunDone || run.Output != "handled" {
		t.Fatalf("run = %#v, want recoverable command output", run)
	}
	var sawCommandResponse bool
	for _, ev := range rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventSystemMessage}}) {
		p, ok := ev.Payload.(MessagePayload)
		if ok && p.Content == "handled" {
			sawCommandResponse = true
		}
	}
	if !sawCommandResponse {
		t.Fatal("command response event not found")
	}
	if got := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventInputAccepted}}); len(got) != 0 {
		t.Fatalf("handled command was projected as user input: %+v", got)
	}
}

func TestManagerCommandDoesNotPublishStaleTurnMetrics(t *testing.T) {
	bot := &testBot{}
	bot.command = func(string, RunHost) CommandResult {
		return CommandResult{Action: CommandHandled, Output: "handled"}
	}
	rt := New(bot, Services{
		ExecuteCommand: bot.ExecuteCommand,
		CommandMenu:    bot.CommandMenu,
		Metrics: func() MetricsSnapshot {
			return MetricsSnapshot{TurnInput: 1000, TurnOutput: 20}
		},
	})

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "/help"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)
	got := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventMetricsUpdated}})
	if len(got) != 0 {
		t.Fatalf("command-only run published stale turn metrics: %+v", got)
	}
}

func TestManagerCommandCanContinueToAgent(t *testing.T) {
	bot := &testBot{}
	bot.command = func(string, RunHost) CommandResult {
		return CommandResult{Action: CommandContinue, Output: "context selected"}
	}
	bot.run = func(string, RunHost) (string, error) {
		return "agent ran", nil
	}
	rt := newTestRuntime(bot)

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "/skill review"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForRun(t, rt, runID)
	run, ok := rt.LookupRun(runID)
	if !ok {
		t.Fatal("run snapshot missing")
	}
	if run.Output != "agent ran" {
		t.Fatalf("run output = %q, want agent ran", run.Output)
	}
	accepted := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventInputAccepted}})
	if len(accepted) != 1 {
		t.Fatalf("continuing command input events = %d, want 1", len(accepted))
	}
}

func TestManagerCommandCompletesInOneLifecycle(t *testing.T) {
	bot := &testBot{}
	bot.command = func(string, RunHost) CommandResult {
		return CommandResult{Action: CommandHandled, Output: "installed"}
	}
	rt := newTestRuntime(bot)

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "/plugin install demo"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForRun(t, rt, runID)
	if run, _ := rt.LookupRun(runID); run.Status != RunDone {
		t.Fatalf("async command run = %#v, want done", run)
	}
	doneEvents := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventRunDone}})
	if len(doneEvents) != 1 {
		t.Fatalf("run_done events = %d, want 1", len(doneEvents))
	}
}

func TestManagerDoesNotDuplicateStreamedFinalText(t *testing.T) {
	bot := &testBot{}
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Text("hello")
		host.Text(" world")
		return "hello world", nil
	}
	rt := newTestRuntime(bot)

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "feishu"}, Text: "hi"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForRun(t, rt, runID)

	var streamed strings.Builder
	for _, ev := range rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventAssistantDelta}}) {
		p, ok := ev.Payload.(DeltaPayload)
		if ok {
			streamed.WriteString(p.Delta)
		}
	}
	if got := streamed.String(); got != "hello world" {
		t.Fatalf("streamed assistant text = %q, want one copy of final text", got)
	}
}

func TestManagerIgnoresUnknownStepEvent(t *testing.T) {
	bot := &testBot{}
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepAction("unknown"), ToolName: "mystery", Output: "ignored"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, ToolName: "read", ToolArgs: "path=a.go", Output: "ok"})
		return "done", nil
	}
	rt := newTestRuntime(bot)

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForRun(t, rt, runID)
	run, ok := rt.LookupRun(runID)
	if !ok {
		t.Fatal("run snapshot missing")
	}
	if len(run.Tools) != 1 {
		t.Fatalf("tools = %+v, want only known step action recorded", run.Tools)
	}
	if run.Tools[0].Name != "read" || run.Tools[0].Status != core.ToolDone {
		t.Fatalf("tool = %+v, want completed read tool", run.Tools[0])
	}
}

func TestManagerPublishesStructuredSubAgentEvents(t *testing.T) {
	bot := &testBot{}
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{
			Action: protocol.StepActionSubAgentStart, SubAgentID: "sub_1", SubAgentType: "explore",
			SubAgentProfile: "explore", SubAgentSkills: []string{"check"}, SubAgentColor: 3,
		})
		host.Step(protocol.StepEvent{
			Action: protocol.StepActionToolStart, ToolName: "web_search",
			SubAgentID: "sub_1", SubAgentColor: 3,
		})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "sub_1"})
		return "done", nil
	}
	rt := newTestRuntime(bot)

	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"}, Text: "research",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)
	events := rt.events.History(EventFilter{RunID: runID})

	var sub SubAgentPayload
	var tool ToolPayload
	for _, event := range events {
		switch event.Type {
		case EventSubAgentStarted:
			sub, _ = event.Payload.(SubAgentPayload)
		case EventToolStarted:
			tool, _ = event.Payload.(ToolPayload)
		}
	}
	if sub.ID != "sub_1" || sub.Type != "explore" || sub.Profile != "explore" ||
		!reflect.DeepEqual(sub.Skills, []string{"check"}) || sub.Color != 3 {
		t.Fatalf("subagent payload = %#v", sub)
	}
	if tool.SubAgentID != "sub_1" || tool.SubAgentColor != 3 {
		t.Fatalf("tool payload = %#v", tool)
	}
}

func TestManagerNormalizesToolOutput(t *testing.T) {
	bot := &testBot{}
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{
			Action: protocol.StepActionExecuteTool, ToolName: "shell",
			Output: "vite\n\rtransforming...",
		})
		return "done", nil
	}
	rt := newTestRuntime(bot)

	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"}, Text: "build",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)
	for _, event := range rt.events.History(EventFilter{RunID: runID}) {
		if event.Type != EventToolCompleted {
			continue
		}
		payload, ok := event.Payload.(ToolPayload)
		if !ok || payload.Output != "vite\ntransforming..." {
			t.Fatalf("tool payload = %#v, want normalized output", event.Payload)
		}
		return
	}
	t.Fatal("tool completed event not found")
}

func TestManagerCarriesTrustedToolDecisionMetadata(t *testing.T) {
	bot := &testBot{}
	bot.run = func(_ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{
			Action: protocol.StepActionToolPreview, ToolName: "shell", CallID: "call-1",
			Output: "make test", Decision: protocol.ToolDecisionJevSafe,
		})
		return "done", nil
	}
	rt := newTestRuntime(bot)
	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "build"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)
	for _, event := range rt.events.History(EventFilter{RunID: runID}) {
		if event.Type != EventToolPreview {
			continue
		}
		payload, ok := event.Payload.(ToolPayload)
		if !ok || payload.Preview != "make test" || payload.Decision != protocol.ToolDecisionJevSafe {
			t.Fatalf("tool preview payload = %#v", event.Payload)
		}
		return
	}
	t.Fatal("tool preview event not found")
}

func TestManagerCustomRuntimeCommand(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	rt.registerCommand("hello", "", func(_ context.Context, args []string) (string, error) {
		return "hello " + strings.Join(args, " "), nil
	})

	_, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"},
		Text:   "/hello world",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	var found bool
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if run, ok := rt.LookupRun(rt.currentRunID()); ok && run.Status == RunDone {
			found = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found {
		t.Fatal("custom runtime command did not finish")
	}

	events := rt.events.History(EventFilter{})
	var sawMessage bool
	for _, ev := range events {
		if ev.Type == EventSystemMessage {
			if payload, ok := ev.Payload.(MessagePayload); ok && payload.Content == "hello world" {
				sawMessage = true
				break
			}
		}
	}
	if !sawMessage {
		t.Fatalf("did not see expected system message in events: %#v", events)
	}
}

func TestManagerCommandCatalogDerivesRootMenu(t *testing.T) {
	rt := newTestRuntime(&testBot{
		commands: []string{"/help", "$review", "model"},
	})

	got := rt.CommandCatalog()
	for _, want := range []string{"/help", "/model", "$review"} {
		if !hasString(got, want) {
			t.Fatalf("CommandCatalog() missing %q in %v", want, got)
		}
	}
}

func TestManagerRootMenuDoesNotAutoSubmitRuntimeCommands(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	menu, ok := rt.CommandMenu(context.Background(), "/")
	if !ok {
		t.Fatal("root menu unavailable")
	}
	for _, item := range menu.Items {
		if item.Submit {
			t.Fatalf("runtime root item %q auto-submits", item.Value)
		}
	}
}

func TestManagerExposesStructuredCommandMenu(t *testing.T) {
	rt := newTestRuntime(&testBot{menu: func(input string) (CommandMenu, bool) {
		if input != "/model" {
			return CommandMenu{}, false
		}
		return CommandMenu{Title: "Models", Items: []CommandMenuItem{{Value: "/model fast", Submit: true}}}, true
	}})

	menu, ok := rt.CommandMenu(context.Background(), "/model")
	if !ok || menu.Title != "Models" || len(menu.Items) != 1 || !menu.Items[0].Submit {
		t.Fatalf("command menu = %+v, %v", menu, ok)
	}
}

func TestManagerBuildsConnectorMenusFromRuntimeState(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	rt.RegisterConnector("demo", func(runtime ConnectorRuntime) Connector {
		return statusPublishingConnector{rt: runtime}
	})

	menu, ok := rt.CommandMenu(context.Background(), "/connect")
	if !ok || menu.Title != "Choose connector" || len(menu.Items) != 1 || menu.Items[0].Value != "/connect demo" || menu.Items[0].Submit {
		t.Fatalf("connect menu = %+v, %v", menu, ok)
	}
	menu, ok = rt.CommandMenu(context.Background(), "/disconnect")
	if !ok || len(menu.Items) != 0 || menu.Empty != "No active connectors" {
		t.Fatalf("disconnect menu = %+v, %v", menu, ok)
	}
}

func TestManagerCommandMenuHonorsCancellationBeforeRuntimeFallback(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	rt.RegisterConnector("demo", func(runtime ConnectorRuntime) Connector {
		return statusPublishingConnector{rt: runtime}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := rt.CommandMenu(ctx, "/connect"); ok {
		t.Fatal("cancelled command menu fell through to runtime choices")
	}
}

func TestManagerRuntimeCommandsRequireSlash(t *testing.T) {
	agentInput := make(chan string, 1)
	bot := &testBot{
		run: func(input string, _ RunHost) (string, error) {
			agentInput <- input
			return "agent ran", nil
		},
	}
	rt := newTestRuntime(bot)
	rt.registerCommand("hello", "", func(_ context.Context, _ []string) (string, error) {
		return "runtime command ran", nil
	})

	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"},
		Text:   "hello world",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForRun(t, rt, runID)

	select {
	case got := <-agentInput:
		if got != "hello world" {
			t.Fatalf("agent input = %q, want hello world", got)
		}
	default:
		t.Fatal("bare runtime command text should run the agent")
	}
}

func TestRunHostRejectsEventsAfterRunFinishes(t *testing.T) {
	var retained RunHost
	bot := &testBot{run: func(_ string, host RunHost) (string, error) {
		retained = host
		host.Text("before")
		return "done", nil
	}}
	rt := newTestRuntime(bot)
	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "run"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)

	retained.Text("after")
	retained.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "late", ToolName: "read"})
	if reply := retained.Confirm(ConfirmRequest{ToolName: "late"}); reply.Allowed {
		t.Fatalf("stale host confirmation = %+v", reply)
	}

	deltas := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventAssistantDelta}})
	if len(deltas) != 1 {
		t.Fatalf("assistant deltas = %d, want only the in-run event", len(deltas))
	}
	tools := rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventToolStarted}})
	if len(tools) != 0 {
		t.Fatalf("stale host published tool events: %#v", tools)
	}
}

func TestManagerRecoversFromRunPanic(t *testing.T) {
	bot := &testBot{
		run: func(string, RunHost) (string, error) {
			panic("intentional runner panic")
		},
	}
	rt := newTestRuntime(bot)

	_, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"},
		Text:   "trigger panic",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	var failedRun RunSnapshot
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if run, ok := rt.LookupRun(rt.currentRunID()); ok && run.Status == RunFailed {
			failedRun = run
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if failedRun.Status != RunFailed {
		t.Fatalf("expected run to fail after panic, got status %q", failedRun.Status)
	}
	if !strings.Contains(failedRun.Error, "panicked") {
		t.Fatalf("expected error to mention panic, got %q", failedRun.Error)
	}
}
