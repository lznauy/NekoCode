package a2aapi

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	controlruntime "nekocode/runtime"
)

func TestConfirmedFinalReplacesPreview(t *testing.T) {
	final := "answer\nverified task disposition"
	rt := &fakeRuntime{eventBatches: [][]controlruntime.Event{{
		{Sequence: 1, Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: "answer"}},
		{Sequence: 2, Type: controlruntime.EventAssistantMessage, Payload: controlruntime.MessagePayload{Content: final}},
		{Sequence: 3, Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: final}},
	}}}
	events := executeEvents(t, newExecutor(rt), &a2asrv.ExecutorContext{TaskID: "task", ContextID: "context", Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work"))})
	output := ""
	for _, event := range events {
		if update, ok := event.(*a2a.TaskArtifactUpdateEvent); ok {
			if !update.Append {
				output = ""
			}
			for _, part := range update.Artifact.Parts {
				output += part.Text()
			}
		}
	}
	if output != final {
		t.Fatalf("confirmed final output lost: got=%q want=%q", output, final)
	}
}
