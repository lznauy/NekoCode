package runtime

import (
	"context"
	"nekocode/protocol"
	"testing"
	"time"
)

func TestResumedTaskInteractionsSurviveOldAttemptCleanup(t *testing.T) {
	for _, question := range []bool{false, true} {
		name := "approval"
		eventType := EventApprovalRequested
		if question {
			name = "question"
			eventType = EventQuestionRequested
		}
		t.Run(name, func(t *testing.T) {
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
			defer rt.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			oldCtx, oldCancel := context.WithCancel(ctx)
			defer oldCancel()
			newCtx, newCancel := context.WithCancel(ctx)
			defer newCancel()
			old := backgroundHost{runtime: rt, ctx: oldCtx, id: "same-task", sessionID: "session", interactionID: "old-scope"}
			current := backgroundHost{runtime: rt, ctx: newCtx, id: "same-task", sessionID: "session", interactionID: "new-scope"}
			setEntry := func(owner RunID, cancel context.CancelFunc) {
				rt.background.mu.Lock()
				rt.background.entries["same-task"] = &backgroundEntry{interactionID: owner, cancel: cancel, BackgroundTask: BackgroundTask{ID: "same-task", SessionID: "session", Status: "running"}}
				rt.background.mu.Unlock()
			}
			events, err := rt.Events(ctx, EventFilter{Types: []EventType{eventType}})
			if err != nil {
				t.Fatal(err)
			}
			request := func(host backgroundHost) bool {
				if question {
					return !host.Ask(protocol.QuestionRequest{Questions: []protocol.QuestionItem{{Question: "Continue?"}}}).Rejected
				}
				return host.Confirm(protocol.ConfirmRequest{ToolName: "shell"}).Allowed
			}
			nextID := func() string {
				select {
				case event := <-events:
					if event.RunID != "task_same-task" {
						t.Errorf("public event routing changed: %s", event.RunID)
					}
					if question {
						return event.Payload.(QuestionView).ID
					}
					return event.Payload.(ApprovalView).ID
				case <-ctx.Done():
					t.Fatal(ctx.Err())
					return ""
				}
			}
			oldReply, newReply := make(chan bool, 1), make(chan bool, 1)
			setEntry(old.interactionID, oldCancel)
			go func() { oldReply <- request(old) }()
			nextID()
			// Expose the resumed entry before the old cancellation callback drains.
			setEntry(current.interactionID, newCancel)
			go func() { newReply <- request(current) }()
			newID := nextID()
			old.Step(protocol.StepEvent{SubAgentAttemptID: "old-attempt", SubAgentAttemptSeq: 1})
			oldCancel()
			select {
			case allowed := <-oldReply:
				if allowed {
					t.Fatal("cancelled old attempt approved")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			rt.approvals.RejectRun(old.interactionID)
			rt.questions.RejectRun(old.interactionID)
			if question {
				if len(rt.questions.Pending()) != 1 {
					t.Fatal("old cleanup rejected new question")
				}
				err = rt.questions.Answer(newID, protocol.QuestionReply{Answers: [][]string{{"Yes"}}})
			} else {
				if len(rt.approvals.Pending()) != 1 {
					t.Fatal("old cleanup rejected new approval")
				}
				err = rt.approvals.Decide(newID, ApprovalDecision{Allowed: true})
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case allowed := <-newReply:
				if !allowed {
					t.Fatal("new interaction was rejected")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			old.Step(protocol.StepEvent{SubAgentAttemptID: "old-attempt", SubAgentAttemptSeq: 1})
			rt.background.mu.Lock()
			defer rt.background.mu.Unlock()
			if rt.background.entries["same-task"].AttemptID != "" {
				t.Fatal("stale callback changed resumed attempt")
			}
		})
	}
}
