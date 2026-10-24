package eventbus

import (
	"strings"
	"testing"

	"nekocode/runtime/internal/core"
)

func TestEvictionUsesOriginalStructuredPayloadCharge(t *testing.T) {
	b := NewEventBus()
	defer b.Close()
	b.historyLimit = 1
	args := map[string]any{"value": "small"}
	b.Publish(core.Event{Payload: core.ApprovalView{Args: args}})
	// Payload maps can also be reached through History or a live subscriber.
	// Billing must subtract the charge that was actually added at insertion.
	args["value"] = strings.Repeat("x", 1<<20)
	b.Publish(core.Event{Payload: core.DeltaPayload{Delta: "new"}})
	want := eventBytes(b.History(core.EventFilter{})[0])
	if b.historyBytes != want {
		t.Fatalf("history bytes = %d, want %d after eviction", b.historyBytes, want)
	}
}
