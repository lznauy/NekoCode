package headless

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"nekocode/internal/testutil/eventcontract"
	rt "nekocode/runtime"
)

func TestSharedLifecycleContract(t *testing.T) {
	for _, scenario := range eventcontract.Scenarios() {
		t.Run(scenario.Name, func(t *testing.T) {
			backend := rt.NewWithServices(rt.RunnerFunc(func(context.Context, string, rt.RunHost) (string, error) { return "", nil }), rt.ServiceGroups{})
			defer backend.Close()
			conn := &transport{ctx: t.Context(), writes: make(chan writeItem, 128), errors: make(chan error, 1)}
			s := &server{backend: backend, conn: conn, active: &turn{runID: "contract-run", cancel: func() {}, tools: make(map[toolKey]*toolState), wireIDs: map[string]bool{}}}
			for _, event := range scenario.Events {
				if err := s.event(t.Context(), event); err != nil {
					t.Fatal(err)
				}
			}
			order := []string{}
			messages := map[string]string{}
			terminalCount := 0
			for len(conn.writes) > 0 {
				var frame map[string]any
				if err := json.Unmarshal((<-conn.writes).data, &frame); err != nil {
					t.Fatal(err)
				}
				if frame["type"] == "result" {
					terminalCount++
					if frame["result"] != scenario.Output {
						t.Fatalf("result=%v", frame)
					}
				}
				if frame["type"] != "assistant" {
					continue
				}
				message := frame["message"].(map[string]any)
				id := message["id"].(string)
				for _, raw := range message["content"].([]any) {
					block := raw.(map[string]any)
					if block["type"] != "text" {
						continue
					}
					if _, exists := messages[id]; !exists {
						order = append(order, id)
					}
					messages[id] = block["text"].(string)
				}
			}
			var text strings.Builder
			for _, id := range order {
				text.WriteString(messages[id])
			}
			if text.String() != scenario.Display || terminalCount != 1 {
				t.Fatalf("display=%q want=%q terminalCount=%d", text.String(), scenario.Display, terminalCount)
			}
		})
	}
}
