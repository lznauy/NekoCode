package subagent

import (
	"encoding/json"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
)

func TestHandoffValidationNeverRewritesWorkerFacts(t *testing.T) {
	for _, status := range []string{"passed", "failed", "blocked", "not_run"} {
		t.Run(status, func(t *testing.T) {
			args := handoffArgs()
			args["risks"] = []any{"confirmed data loss on retry", "stale writes remain possible"}
			check := verificationArgs(status)
			check["method"] = "executed"
			args["verification_checks"] = []any{check}
			before, _ := json.Marshal(args)
			h, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}})
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(args)
			if string(before) != string(after) {
				t.Fatal("validation mutated original tool-call arguments")
			}
			if len(h.Risks) != 2 || h.Risks[0] != "confirmed data loss on retry" || h.Risks[1] != "stale writes remain possible" || h.Checks[0].Status != status || len(h.Checks[0].Limitations) != 1 {
				t.Fatalf("reported facts changed: %+v", h)
			}
		})
	}
}
