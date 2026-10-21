package runner

import (
	"context"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
)

func TestExecutorNormalizesTerminalControlCharacters(t *testing.T) {
	input := "$ vite build\n\x1b[2Kvite v8\n\rtransforming..."
	want := "$ vite build\nvite v8\ntransforming..."
	e := newTestExecutor(fakeRegistry{"task": fakeTool{name: "task", mode: core.ModeParallel, output: input}})
	result := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "task"}})[0]
	if result.Error != "" || result.Output != want {
		t.Fatalf("ExecuteBatch() = %+v, want output %q", result, want)
	}
}
