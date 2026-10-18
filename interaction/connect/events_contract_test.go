package connect

import (
	"testing"

	"nekocode/internal/testutil/eventcontract"
	rt "nekocode/runtime"
)

func TestSharedLifecycleContract(t *testing.T) {
	for _, scenario := range eventcontract.Scenarios() {
		t.Run(scenario.Name, func(t *testing.T) {
			var terminal []Intent
			resets, wantResets := 0, 0
			for _, event := range scenario.Events {
				if event.Type == rt.EventAssistantReset {
					wantResets++
				}
				for _, intent := range Translate(event) {
					if intent.Kind == IntentPreviewReset {
						resets++
					}
					if intent.Kind == IntentResult || intent.Kind == IntentFailed || intent.Kind == IntentStopped {
						terminal = append(terminal, intent)
					}
				}
			}
			if resets != wantResets || len(terminal) != 1 {
				t.Fatalf("resets=%d/%d terminal=%v", resets, wantResets, terminal)
			}
			wantKind := map[rt.EventType]IntentKind{rt.EventRunDone: IntentResult, rt.EventRunFailed: IntentFailed, rt.EventRunCancelled: IntentStopped}[scenario.Terminal]
			if terminal[0].Kind != wantKind {
				t.Fatalf("terminal=%v", terminal)
			}
			if wantKind == IntentResult && terminal[0].Text != scenario.Output {
				t.Fatalf("output=%q", terminal[0].Text)
			}
		})
	}
}
