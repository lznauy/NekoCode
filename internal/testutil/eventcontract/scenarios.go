// Package eventcontract supplies shared lifecycle scenarios to adapter tests.
// Expected wire shapes differ; committed text and terminal meaning do not.
package eventcontract

import (
	"time"

	rt "nekocode/runtime"
)

type Scenario struct {
	Name           string
	Events         []rt.Event
	Display        string
	Output         string
	SnapshotOutput string
	Terminal       rt.EventType
}

func Scenarios() []Scenario {
	delta := func(text string) rt.Event {
		return rt.Event{Type: rt.EventAssistantDelta, Payload: rt.DeltaPayload{Delta: text}}
	}
	message := func(text string) rt.Event {
		return rt.Event{Type: rt.EventAssistantMessage, Payload: rt.MessagePayload{Role: "assistant", Content: text}}
	}
	done := func(text string) rt.Event {
		return rt.Event{Type: rt.EventRunDone, Payload: rt.RunResult{Output: text}}
	}
	reset := rt.Event{Type: rt.EventAssistantReset}
	cases := []Scenario{
		{Name: "plain", Events: []rt.Event{delta("answer"), message("answer"), done("answer")}, Display: "answer", Output: "answer"},
		{Name: "retry", Events: []rt.Event{delta("discarded"), reset, delta("answer"), message("answer"), done("answer")}, Display: "answer", Output: "answer"},
		{Name: "canonical correction", Events: []rt.Event{delta("wrong"), message("correct"), done("correct")}, Display: "correct", Output: "correct"},
		{Name: "returned output", Events: []rt.Event{delta("preview"), done("answer")}, Display: "answer", Output: "answer"},
		{Name: "tool preamble", Events: []rt.Event{delta("checking"), message("checking"), {Type: rt.EventToolStarted, Payload: rt.ToolPayload{ToolName: "read", CallID: "call"}}, {Type: rt.EventToolCompleted, Payload: rt.ToolPayload{ToolName: "read", CallID: "call"}}, delta("discarded"), reset, message("answer"), done("answer")}, Display: "checkinganswer", Output: "answer"},
		{Name: "blocked tool", Events: []rt.Event{{Type: rt.EventToolBlocked, Payload: rt.ToolPayload{ToolName: "shell", CallID: "call", IsError: true, Output: "denied"}}, message("denied"), done("denied")}, Display: "denied", Output: "denied"},
		{Name: "failure", Events: []rt.Event{message("checking"), delta("discarded"), reset, {Type: rt.EventRunFailed, Payload: rt.RunResult{Error: "failed"}}}, Display: "checking", SnapshotOutput: "checking"},
		{Name: "cancellation", Events: []rt.Event{message("checking"), delta("discarded"), reset, {Type: rt.EventRunCancelled, Payload: rt.RunResult{Error: "cancelled"}}}, Display: "checking", SnapshotOutput: "checking"},
	}
	for i := range cases {
		cases[i].Terminal = cases[i].Events[len(cases[i].Events)-1].Type
		if cases[i].Terminal == rt.EventRunDone {
			cases[i].SnapshotOutput = cases[i].Output
		}
		for j := range cases[i].Events {
			cases[i].Events[j].RunID = "contract-run"
			cases[i].Events[j].Sequence = uint64(j + 1)
			cases[i].Events[j].Time = time.Unix(int64(j+1), 0)
		}
	}
	return cases
}
