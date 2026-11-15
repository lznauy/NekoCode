package runtime

import (
	"context"
	"nekocode/mode"
	"nekocode/protocol"
	"strings"
	"testing"
	"time"
)

func TestBatchDeliveryUsesGenericSchedulingOutcomeAcrossDomains(t *testing.T) {
	for _, domain := range []string{"development", "research", "review"} {
		for _, outcome := range []string{"complete", "continue", "blocked", "decision"} {
			t.Run(domain+"/"+outcome, func(t *testing.T) {
				report := protocol.TaskReview{TaskReport: protocol.TaskReport{Summary: domain + " evidence", Outcome: outcome, NextAction: "remaining action", Unfinished: []string{"not done"}}}
				status := "partial"
				if outcome == "complete" {
					status = "completed"
					report.Unfinished = nil
				}
				if err := protocol.ValidateHandoff(report.Outcome, report.NextAction, report.Unfinished); err != nil {
					t.Fatal(err)
				}
				a := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "a", SessionID: "s", GroupID: "g", Status: status, ReviewPolicy: "batch", Review: &report, ReviewComplete: true}}
				peer := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "peer", SessionID: "s", GroupID: "g", Status: "running", ReviewPolicy: "batch"}}
				b := &backgroundTasks{entries: map[string]*backgroundEntry{"a": a, "peer": peer}, order: []string{"a", "peer"}}
				ids, _ := b.reviewIDsLocked("s")
				wantImmediate := outcome == "blocked" || outcome == "decision"
				if (len(ids) == 1) != wantImmediate {
					t.Fatalf("%s routed incorrectly: %v", outcome, ids)
				}
				peer.Status = "completed"
				peer.Notified = true
				ids, _ = b.reviewIDsLocked("s")
				if len(ids) != 1 {
					t.Fatal("settled stage never delivered")
				}
			})
		}
	}
}
func TestBatchDeliveryPreservesLegacyUrgencyAndErrors(t *testing.T) {
	legacy := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "a", SessionID: "s", GroupID: "g", Status: "partial", ReviewPolicy: "batch"}}
	peer := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "peer", SessionID: "s", GroupID: "g", Status: "running", ReviewPolicy: "batch"}}
	b := &backgroundTasks{entries: map[string]*backgroundEntry{"a": legacy, "peer": peer}, order: []string{"a", "peer"}}
	ids, _ := b.reviewIDsLocked("s")
	if len(ids) != 1 {
		t.Fatal("legacy partial was silently delayed")
	}
	legacy.Review = &protocol.TaskReview{TaskReport: protocol.TaskReport{Outcome: "continue", NextAction: "next"}}
	legacy.Error = "save failed"
	if batchableHandoff(legacy.BackgroundTask) {
		t.Fatal("host failure suppressed by worker outcome")
	}
}
func TestOversizedGenericHandoffRetainsRoutingAndFullEvidenceRecovery(t *testing.T) {
	r := protocol.TaskReview{TaskReport: protocol.TaskReport{Outcome: "continue", NextAction: "check remaining input then stop", Unfinished: []string{"remaining"}, Summary: strings.Repeat("long evidence ", 1000)}}
	copy, complete := boundedTaskReview(r)
	if complete || copy.Outcome != "continue" || copy.NextAction != r.NextAction {
		t.Fatal("scheduling metadata lost during bounding")
	}
	task := BackgroundTask{Status: "partial", Review: copy, ReviewComplete: false}
	if !batchableHandoff(task) {
		t.Fatal("bounded report confused with decision blocker")
	}
}

func TestIndependentContinuationBatchWakesCoordinatorOnce(t *testing.T) {
	notices := make(chan string, 4)
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		notices <- input
		return "combined stage result", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	a, b := make(chan struct{}), make(chan struct{})
	for _, spec := range []struct {
		id      string
		release chan struct{}
	}{{"implementation", a}, {"research", b}} {
		_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: spec.id, SessionID: "session", GroupID: "stage", ReviewPolicy: "batch", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			select {
			case <-spec.release:
			case <-ctx.Done():
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}
			review := protocol.TaskReview{TaskReport: protocol.TaskReport{Outcome: "continue", NextAction: "continue saved workspace after accepting this stage", Summary: "stage artifact saved", Unfinished: []string{"next stage"}, Verification: "not run; no decision needed now"}}
			host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentStatus: "partial"})
			return protocol.BackgroundResult{Review: &review}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	close(a)
	waitBackgroundStatus(t, rt, "implementation", "partial")
	select {
	case <-notices:
		t.Fatal("ordinary continuation prematurely woke coordinator")
	default:
	}
	close(b)
	select {
	case input := <-notices:
		review := decodeBatchNotice(t, input)
		if len(review.Results) != 2 || review.PendingTasks != 0 {
			t.Fatalf("stage evidence split or incomplete: %+v", review)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("settled stage did not wake coordinator")
	}
}

func TestPublicBackgroundHostNonCompleteOutcomeCannotSatisfyDependencies(t *testing.T) {
	for _, outcome := range []string{"", "continue", "blocked", "decision"} {
		t.Run(outcome, func(t *testing.T) {
			services := backgroundServices(t.TempDir())
			services.Modes.Current = func() mode.ID { return mode.Standard }
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
			defer rt.Close()
			ran := make(chan struct{}, 1)
			_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "source", SessionID: "session", GroupID: "g", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (protocol.BackgroundResult, error) {
				review := protocol.TaskReview{TaskReport: protocol.TaskReport{Summary: "stage report", Outcome: outcome, NextAction: "remaining action", Unfinished: []string{"not complete"}}}
				return protocol.BackgroundResult{Review: &review}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			waitBackgroundStatus(t, rt, "source", "partial")
			_, err = rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "dependent", SessionID: "session", GroupID: "g", After: []string{"source"}, ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (protocol.BackgroundResult, error) {
				ran <- struct{}{}
				return protocol.BackgroundResult{Text: "should not run"}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			waitBackgroundStatus(t, rt, "dependent", "blocked")
			select {
			case <-ran:
				t.Fatal("partial source satisfied successful dependency")
			default:
			}
		})
	}
}

func TestOversizedLegacyHandoffKeepsIncompleteSettlement(t *testing.T) {
	full := protocol.TaskReview{TaskReport: protocol.TaskReport{Summary: strings.Repeat("large result ", 1000), Unfinished: []string{"remaining action"}}}
	preview, complete := boundedTaskReview(full)
	if complete || !protocol.HandoffIncomplete(preview.Outcome, preview.Unfinished) {
		t.Fatal("bounding removed completion semantics")
	}
}

func TestExplicitCompletionBatchesRegardlessOfQualityMetadata(t *testing.T) {
	report := protocol.TaskReview{TaskReport: protocol.TaskReport{Outcome: "complete", Summary: "scope delivered", VerificationGaps: []string{"external deployment not checked"}, Risks: []string{"operator review required"}}}
	task := BackgroundTask{Status: "completed", ReviewPolicy: "batch", Review: &report}
	if !batchableHandoff(task) {
		t.Fatal("quality metadata overrode explicit scheduling state")
	}
	task.Error = "persist failed"
	if batchableHandoff(task) {
		t.Fatal("host failure was hidden by completion")
	}
}

func TestLegacyCompletedReportDoesNotWaitForBatch(t *testing.T) {
	report := cleanReviewFixture()
	report.Outcome = ""
	a := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "a", SessionID: "s", GroupID: "g", Status: "completed", ReviewPolicy: "batch", Review: &report, ReviewComplete: true}}
	peer := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "peer", SessionID: "s", GroupID: "g", Status: "running", ReviewPolicy: "batch"}}
	b := &backgroundTasks{entries: map[string]*backgroundEntry{"a": a, "peer": peer}, order: []string{"a", "peer"}}
	ids, _ := b.reviewIDsLocked("s")
	if len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("legacy report delayed behind running peer: %v", ids)
	}
}
