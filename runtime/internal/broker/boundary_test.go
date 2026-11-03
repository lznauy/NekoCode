package broker

import (
	"nekocode/protocol"
	"nekocode/runtime/internal/core"
	"nekocode/runtime/internal/eventbus"
	"testing"
	"time"
)

func TestBrokerApprovalPendingIsDetached(t *testing.T) {
	b := NewApprovalBroker(nil, core.SourceRef{}, nil)
	wait := b.Register(protocol.ConfirmRequest{ToolName: "shell", Args: map[string]any{"nested": map[string]any{"value": "original"}}, Approval: &protocol.ApprovalContext{Scope: protocol.ApprovalScopeOnce, RememberRules: []protocol.ApprovalRememberRule{{Tool: "shell", Specifier: "original"}}}})
	first := b.Pending()[0]
	first.Args["nested"].(map[string]any)["value"] = "changed"
	first.Approval.Scope = protocol.ApprovalScopeProject
	first.Approval.RememberRules[0].Specifier = "changed"
	again := b.Pending()[0]
	if again.Args["nested"].(map[string]any)["value"] != "original" || again.Approval.Scope != protocol.ApprovalScopeOnce || again.Approval.RememberRules[0].Specifier != "original" {
		t.Error("Pending exposes live approval state")
	}
	if err := b.Decide(first.ID, core.ApprovalDecision{Allowed: true, Remember: true}); err == nil {
		t.Error("mutating Pending enabled a remembered decision")
	}
	b.Close()
	_ = wait()
}

func TestBrokerQuestionPendingIsDetached(t *testing.T) {
	b := NewQuestionBroker(nil, core.SourceRef{}, nil)
	wait := b.Register(protocol.QuestionRequest{Questions: []protocol.QuestionItem{{Question: "original", Options: []protocol.QuestionOption{{Label: "original"}}}}})
	first := b.Pending()[0]
	first.Questions[0].Question = "changed"
	first.Questions[0].Options[0].Label = "changed"
	again := b.Pending()[0]
	if again.Questions[0].Question != "original" || again.Questions[0].Options[0].Label != "original" {
		t.Error("Pending exposes live question state")
	}
	b.Close()
	_ = wait()
}

func TestBrokerApprovalResolvedObserverCanReenter(t *testing.T) {
	bus := eventbus.NewEventBus()
	b := NewApprovalBroker(bus, core.SourceRef{}, nil)
	wait := b.Register(protocol.ConfirmRequest{ToolName: "shell"})
	id := b.Pending()[0].ID
	done := make(chan error, 1)
	bus.AddObserver(func(event core.Event) {
		if event.Type == core.EventApprovalRequested && event.Payload.(core.ApprovalView).ToolName == "nested-request" {
			_ = b.Decide(event.Payload.(core.ApprovalView).ID, core.ApprovalDecision{Allowed: true})
			return
		}
		if event.Type != core.EventApprovalResolved || event.Payload.(core.ApprovalView).ID != id {
			return
		}
		if len(b.Pending()) != 0 {
			t.Error("resolved approval remains pending")
		}
		if err := b.Decide(id, core.ApprovalDecision{}); err == nil {
			t.Error("duplicate decision succeeded")
		}
		if !wait().Allowed {
			t.Error("observer could not receive committed reply")
		}
		if !b.Request(protocol.ConfirmRequest{ToolName: "nested-request"}).Allowed {
			t.Error("reentrant request failed")
		}
		nested := b.Register(protocol.ConfirmRequest{ToolName: "nested"})
		b.Close()
		if !nested().Cancelled {
			t.Error("close did not cancel nested request")
		}
	})
	go func() { done <- b.Decide(id, core.ApprovalDecision{Allowed: true}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("resolved observer deadlocked")
	}
	if got := len(bus.History(core.EventFilter{Types: []core.EventType{core.EventApprovalResolved}})); got != 3 {
		t.Fatalf("resolved events=%d, want 3", got)
	}
}

func TestBrokerQuestionResolvedObserverCanReenter(t *testing.T) {
	bus := eventbus.NewEventBus()
	b := NewQuestionBroker(bus, core.SourceRef{}, nil)
	wait := b.Register(protocol.QuestionRequest{})
	id := b.Pending()[0].ID
	done := make(chan error, 1)
	bus.AddObserver(func(event core.Event) {
		if event.Type == core.EventQuestionRequested {
			questions := event.Payload.(core.QuestionView).Questions
			if len(questions) > 0 && questions[0].Question == "nested-request" {
				_ = b.Answer(event.Payload.(core.QuestionView).ID, protocol.QuestionReply{})
				return
			}
		}
		if event.Type != core.EventQuestionResolved || event.Payload.(core.QuestionView).ID != id {
			return
		}
		if len(b.Pending()) != 0 {
			t.Error("resolved question remains pending")
		}
		if err := b.Answer(id, protocol.QuestionReply{}); err == nil {
			t.Error("duplicate answer succeeded")
		}
		if wait().Rejected {
			t.Error("observer could not receive committed reply")
		}
		if b.Request(protocol.QuestionRequest{Questions: []protocol.QuestionItem{{Question: "nested-request"}}}).Rejected {
			t.Error("reentrant request failed")
		}
		nested := b.Register(protocol.QuestionRequest{})
		b.Close()
		if !nested().Rejected {
			t.Error("close did not reject nested request")
		}
	})
	go func() { done <- b.Answer(id, protocol.QuestionReply{}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("resolved observer deadlocked")
	}
	if got := len(bus.History(core.EventFilter{Types: []core.EventType{core.EventQuestionResolved}})); got != 3 {
		t.Fatalf("resolved events=%d, want 3", got)
	}
}

func TestBrokerResolutionRacesCancelExactlyOnce(t *testing.T) {
	for range 25 {
		bus := eventbus.NewEventBus()
		b := NewApprovalBroker(bus, core.SourceRef{}, nil)
		wait := b.Register(protocol.ConfirmRequest{ToolName: "shell"})
		id := b.Pending()[0].ID
		done := make(chan struct{}, 2)
		go func() { _ = b.Decide(id, core.ApprovalDecision{Allowed: true}); done <- struct{}{} }()
		go func() { b.Close(); done <- struct{}{} }()
		<-done
		<-done
		_ = wait()
		if len(b.Pending()) != 0 || len(bus.History(core.EventFilter{Types: []core.EventType{core.EventApprovalResolved}})) != 1 {
			t.Fatal("approval resolved more than once")
		}
		qbus := eventbus.NewEventBus()
		q := NewQuestionBroker(qbus, core.SourceRef{}, nil)
		qwait := q.Register(protocol.QuestionRequest{})
		qid := q.Pending()[0].ID
		go func() { _ = q.Answer(qid, protocol.QuestionReply{}); done <- struct{}{} }()
		go func() { q.Close(); done <- struct{}{} }()
		<-done
		<-done
		_ = qwait()
		if len(q.Pending()) != 0 || len(qbus.History(core.EventFilter{Types: []core.EventType{core.EventQuestionResolved}})) != 1 {
			t.Fatal("question resolved more than once")
		}
	}
}

func TestBrokerQuestionAnswerIsDetached(t *testing.T) {
	b := NewQuestionBroker(nil, core.SourceRef{}, nil)
	wait := b.Register(protocol.QuestionRequest{})
	reply := protocol.QuestionReply{Answers: [][]string{{"original"}}}
	if err := b.Answer(b.Pending()[0].ID, reply); err != nil {
		t.Fatal(err)
	}
	reply.Answers[0][0] = "changed"
	if got := wait(); got.Answers[0][0] != "original" {
		t.Fatal("reply shares caller-owned answers")
	}
}

func TestBrokerCloseCommitsAllRepliesBeforeObservers(t *testing.T) {
	bus := eventbus.NewEventBus()
	b := NewApprovalBroker(bus, core.SourceRef{}, nil)
	first := b.Register(protocol.ConfirmRequest{})
	second := b.Register(protocol.ConfirmRequest{})
	observed := false
	bus.AddObserver(func(event core.Event) {
		if event.Type != core.EventApprovalResolved || observed {
			return
		}
		observed = true
		if !first().Cancelled || !second().Cancelled {
			t.Error("close did not commit all cancellation replies")
		}
	})
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close observer deadlocked waiting for another reply")
	}
	qbus := eventbus.NewEventBus()
	q := NewQuestionBroker(qbus, core.SourceRef{}, nil)
	qfirst := q.Register(protocol.QuestionRequest{})
	qsecond := q.Register(protocol.QuestionRequest{})
	qobserved := false
	qbus.AddObserver(func(event core.Event) {
		if event.Type != core.EventQuestionResolved || qobserved {
			return
		}
		qobserved = true
		if !qfirst().Rejected || !qsecond().Rejected {
			t.Error("close did not commit all question replies")
		}
	})
	qdone := make(chan struct{})
	go func() { q.Close(); close(qdone) }()
	select {
	case <-qdone:
	case <-time.After(time.Second):
		t.Fatal("question close observer deadlocked")
	}
}

func TestBrokerRequestedObserversSeeRequestBeforeResolution(t *testing.T) {
	t.Run("approval", func(t *testing.T) {
		bus := eventbus.NewEventBus()
		b := NewApprovalBroker(bus, core.SourceRef{}, nil)
		var seen []core.EventType
		bus.AddObserver(func(event core.Event) {
			if event.Type == core.EventApprovalRequested {
				b.Close()
			}
		})
		bus.AddObserver(func(event core.Event) { seen = append(seen, event.Type) })
		wait := b.Register(protocol.ConfirmRequest{})
		if !wait().Cancelled {
			t.Fatal("close did not cancel request")
		}
		if len(seen) != 2 || seen[0] != core.EventApprovalRequested || seen[1] != core.EventApprovalResolved {
			t.Fatalf("observer order=%v", seen)
		}
	})
	t.Run("question", func(t *testing.T) {
		bus := eventbus.NewEventBus()
		b := NewQuestionBroker(bus, core.SourceRef{}, nil)
		var seen []core.EventType
		bus.AddObserver(func(event core.Event) {
			if event.Type == core.EventQuestionRequested {
				b.Close()
			}
		})
		bus.AddObserver(func(event core.Event) { seen = append(seen, event.Type) })
		wait := b.Register(protocol.QuestionRequest{})
		if !wait().Rejected {
			t.Fatal("close did not reject question")
		}
		if len(seen) != 2 || seen[0] != core.EventQuestionRequested || seen[1] != core.EventQuestionResolved {
			t.Fatalf("observer order=%v", seen)
		}
	})
}
