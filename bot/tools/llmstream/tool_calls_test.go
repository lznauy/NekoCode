package llmstream

import "testing"

func TestCollectToolCallsSortsByDeltaIndex(t *testing.T) {
	stream := StreamResult{TcAccum: map[int]*ToolAccum{
		1: {ID: "b", Name: "write"},
		0: {ID: "a", Name: "read"},
	}}
	stream.TcAccum[1].Args.WriteString(`{"path":"b.go"}`)
	stream.TcAccum[0].Args.WriteString(`{"path":"a.go"}`)

	calls := stream.CollectToolCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].ID != "a" || calls[1].ID != "b" {
		t.Fatalf("calls not sorted by index: %+v", calls)
	}
}

func TestCollectToolCallsSkipsInvalidArgs(t *testing.T) {
	stream := StreamResult{TcAccum: map[int]*ToolAccum{
		0: {ID: "bad", Name: "read"},
		1: {ID: "ok", Name: "read"},
	}}
	stream.TcAccum[0].Args.WriteString(`{`)
	stream.TcAccum[1].Args.WriteString(`{"path":"a.go"}`)

	calls := stream.CollectToolCalls()
	if len(calls) != 1 || calls[0].ID != "ok" {
		t.Fatalf("expected only valid call, got %+v", calls)
	}
}
