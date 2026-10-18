package acp

import (
	"strings"
	"testing"

	"nekocode/internal/testutil/eventcontract"
)

func TestTurnUpdatesCanonicalTextContract(t *testing.T) {
	for _, tc := range eventcontract.Scenarios() {
		t.Run(tc.Name, func(t *testing.T) {
			var state turnUpdates
			var output strings.Builder
			for _, event := range tc.Events {
				updates, _ := state.update(event)
				for _, update := range updates {
					if update["sessionUpdate"] == "agent_message_chunk" {
						output.WriteString(update["content"].(map[string]any)["text"].(string))
					}
				}
			}
			if got := output.String(); got != tc.Display {
				t.Fatalf("text = %q, want %q", got, tc.Display)
			}
		})
	}
}
