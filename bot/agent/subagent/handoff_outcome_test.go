package subagent

import (
	"encoding/json"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"strings"
	"testing"
)

func TestGenericHandoffOutcomesAcrossTaskDomains(t *testing.T) {
	for _, domain := range []string{"implement game controls", "research competing approaches", "review changed code"} {
		for _, outcome := range []string{"", "complete", "continue", "blocked", "decision"} {
			t.Run(domain+"/"+outcome, func(t *testing.T) {
				args := handoffArgs()
				if outcome != "" {
					args["outcome"] = outcome
				}
				args["summary"] = domain
				if outcome != "" && outcome != "complete" {
					args["next_action"] = "perform next scoped check; stop on one decisive result"
					args["unfinished"] = []any{"remaining task"}
				}
				raw, _ := json.Marshal(args)
				model := &scriptedLLM{scripts: [][]types.StreamToken{{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "handoff", Name: submitResultToolName, Arguments: string(raw)}}}}}
				result, err := New(Config{LLM: model, Tools: tools.New()}).Run(t.Context(), RunConfig{Prompt: domain, Profile: Profile{Name: "executor"}})
				if err != nil || result.Handoff == nil || result.Handoff.Outcome != outcome {
					t.Fatalf("%+v %v", result, err)
				}
				if (result.Status == StatusCompleted) != (outcome == "" || outcome == "complete") {
					t.Fatal("scheduling handoff promoted unfinished work to completion")
				}
				if outcome != "" && !strings.Contains(result.Content, "Handoff outcome: "+outcome) {
					t.Fatal("outcome lost in durable canonical report")
				}
			})
		}
	}
}
func TestGenericHandoffRejectsMalformedOutcomesAndUsesUnfinishedAsTruth(t *testing.T) {
	for _, outcome := range []any{nil, "", "unknown", 1} {
		args := handoffArgs()
		args["outcome"] = outcome
		if _, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}}); err == nil {
			t.Fatal("invalid outcome accepted")
		}
	}
	for _, outcome := range []string{"continue", "blocked", "decision"} {
		args := handoffArgs()
		args["outcome"] = outcome
		if _, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}}); err == nil {
			t.Fatal("non-complete handoff omitted next_action and unfinished")
		}
		args["next_action"] = "next step"
		if _, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}}); err == nil {
			t.Fatal("non-complete handoff omitted unfinished")
		}
	}
	args := handoffArgs()
	args["outcome"] = "complete"
	args["unfinished"] = []any{"not done"}
	_, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}})
	if err == nil {
		t.Fatal("contradictory completion was accepted")
	}
}
func TestGenericWorkerSchemaHasNoMandatoryReviewWorkflow(t *testing.T) {
	engine := New(Config{Tools: tools.New()})
	defs := engine.filteredToolDefs(nil)
	parameters := defs[len(defs)-1].Function.Parameters
	found := false
	for _, field := range parameters.Required {
		if field == "outcome" {
			found = true
		}
	}
	if !found {
		t.Fatal("new workers can omit scheduling outcome")
	}
	fields := defs[len(defs)-1].Function.Parameters.Properties
	if _, ok := fields["findings"]; ok {
		t.Fatal("domain-specific findings leaked into generic schema")
	}
	if _, ok := fields["outcome"]; !ok {
		t.Fatal("generic scheduling outcome missing")
	}
	legacy := handoffArgs()
	h, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: legacy}})
	if err != nil || h.Outcome != "" {
		t.Fatal("legacy handoff changed")
	}
}
