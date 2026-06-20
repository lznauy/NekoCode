package governance

import (
	"testing"

	"nekocode/bot/hooks"
)

func TestGovResetTurnBetweenPublishesQuotaAndInput(t *testing.T) {
	g := NewManager(hooks.NewRegistry())
	g.HookReg.Register(hooks.Hook{
		Name:  "assert-state",
		Point: hooks.PreTurn,
		On: func(s *hooks.Snapshot) *hooks.Result {
			if s.Store[hooks.StoreQuotaReads] != 5 {
				t.Fatalf("quota reads = %d, want 5", s.Store[hooks.StoreQuotaReads])
			}
			if s.Store[hooks.StoreStepInputLen] != 5 {
				t.Fatalf("input len = %d, want 5", s.Store[hooks.StoreStepInputLen])
			}
			return nil
		},
	})

	g.ResetTurnBetween("hello", QuotaData{MaxSlots: 8, Used: 3})

	g.HookReg.Evaluate(hooks.PreTurn, "", false)
}

func TestGovResetClearsTrackingState(t *testing.T) {
	g := NewManager(hooks.NewRegistry())
	g.prevReads = 1
	g.prevModifies = 2
	g.prevVerifications = 3

	g.Reset()

	if g.prevReads != 0 || g.prevModifies != 0 || g.prevVerifications != 0 {
		t.Fatalf("tracking state not reset: %+v", g)
	}
}
