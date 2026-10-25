package eventbus

import (
	"nekocode/runtime/internal/core"
	"testing"
)

func TestNormalizeRecordedEventPreservesExplicitFields(t *testing.T) {
	for _, tt := range []struct {
		event    core.Event
		sequence uint64
		version  string
	}{
		{core.Event{ID: "evt_2"}, 2, core.ProtocolVersion},
		{core.Event{ID: "evt_2", Sequence: 7, Version: "explicit"}, 7, "explicit"},
		{core.Event{ID: "invalid"}, 0, core.ProtocolVersion},
	} {
		got := NormalizeRecordedEvent(tt.event)
		if got.Sequence != tt.sequence || got.Version != tt.version {
			t.Fatalf("normalized %+v to %+v", tt.event, got)
		}
	}
}
