package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Scripted replies validate the harness, not whether a real model reasons well.
type reviewReplay struct {
	tc            reviewCase
	system, skill string
	verdict       string
	step          int
}

func (r *reviewReplay) Chat(_ context.Context, messages []message, _ []toolDef) (*modelResponse, error) {
	if messages[0].Content != r.system {
		return nil, fmt.Errorf("mixed system versions")
	}
	answer := message{Role: "assistant", Content: r.verdict}
	if r.step == 0 {
		answer.Content = ""
		makeCall := func(name string, args map[string]string) {
			data, _ := json.Marshal(args)
			answer.ToolCalls = append(answer.ToolCalls, toolCall{ID: fmt.Sprint(len(answer.ToolCalls)), Type: "function", Function: functionCall{Name: name, Arguments: string(data)}})
		}
		makeCall("skill", map[string]string{"name": "check"})
		for _, path := range reviewPaths(r.tc.files) {
			makeCall("read", map[string]string{"path": path})
		}
	} else {
		if !slices.ContainsFunc(messages, func(m message) bool { return m.Role == "tool" && strings.Contains(m.Content, r.skill) }) {
			return nil, fmt.Errorf("mixed skill versions")
		}
	}
	r.step++
	return &modelResponse{Choices: []choice{{Message: answer}}}, nil
}

func fixtureReviewVerdict(tc reviewCase, withFinding bool) string {
	verdict := reviewVerdict{Findings: []reviewFinding{}}
	if withFinding {
		quote := "const clearMessages = () => setMsgs([]);"
		trigger := "删除当前会话后"
		reason := "clearMessages 直接调用 React setter，绕过 setMessages 的缓冲和 ref 清理。"
		if tc.targetPath == "confirm.go" {
			quote = `lines := []string{"需要确认"}`
			trigger = "申请非空 Capabilities 时"
			reason = "permissionSummary 始终返回固定提示，未读取或显示能力范围。"
		}
		if !strings.Contains(tc.files[tc.targetPath], quote) {
			// An evidence-backed but wrong conclusion must still count as a false positive.
			quote = strings.Split(tc.files[tc.targetPath], "\n")[0]
		}
		verdict.Findings = append(verdict.Findings, reviewFinding{Path: tc.targetPath, Symbol: tc.targetSymbol, Trigger: trigger, Reason: reason, Evidence: []reviewEvidence{{Path: tc.targetPath, Quote: quote}}})
	}
	data, _ := json.Marshal(verdict)
	return string(data)
}

func TestReviewScoringAndReplay(t *testing.T) {
	for _, tc := range reviewCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"baseline", "current"} {
				skill := name + "-check"
				variant := behaviorVariant{version: behaviorVersion{Name: name}, system: name + "-system", manager: &workflowSet{skills: map[string]*workflow{"check": {Name: "check", Content: skill}}}}
				replay := &reviewReplay{tc: tc, system: variant.system, skill: skill, verdict: fixtureReviewVerdict(tc, tc.defective)}
				result := runReviewCase(t, replay, variant, tc)
				if result.InfrastructureError != "" || len(result.Issues) != 0 {
					t.Fatalf("correct replay failed: %+v", result)
				}
				replay = &reviewReplay{tc: tc, system: variant.system, skill: skill, verdict: fixtureReviewVerdict(tc, !tc.defective)}
				result = runReviewCase(t, replay, variant, tc)
				want := "false_positive"
				if tc.defective {
					want = "false_negative"
				}
				if !slices.Contains(result.Issues, want) {
					t.Fatalf("wrong verdict escaped scoring: %+v", result)
				}
			}
		})
	}
}

func TestReviewEvidenceAndToolBoundaries(t *testing.T) {
	tc := reviewCases()[1]
	f := &reviewFixture{tc: tc, read: map[string]bool{}}
	answer := fixtureReviewVerdict(tc, true)
	issues := scoreReview(f, answer)
	for _, want := range []string{"check_not_loaded", "source_not_read:chat.ts", "unsupported_evidence"} {
		if !slices.Contains(issues, want) {
			t.Fatalf("unverified answer accepted: %v", issues)
		}
	}
	for path := range tc.files {
		f.read[path] = true
	}
	f.check = true
	for _, invalid := range []string{"", "{}", `{"findings":null}`, "plain explanation"} {
		if !slices.Contains(scoreReview(f, invalid), "invalid_verdict") {
			t.Fatalf("invalid verdict accepted: %q", invalid)
		}
	}
	var fabricated reviewVerdict
	if err := json.Unmarshal([]byte(answer), &fabricated); err != nil {
		t.Fatal(err)
	}
	fabricated.Findings[0].Evidence[0].Quote = "invented source"
	encoded, err := json.Marshal(fabricated)
	if err != nil {
		t.Fatal(err)
	}
	answer = string(encoded)
	if !slices.Contains(scoreReview(f, answer), "unsupported_evidence") {
		t.Fatal("fabricated evidence accepted")
	}
	for _, call := range []toolCall{
		{Function: functionCall{Name: "read", Arguments: `{"path":"../secrets"}`}},
		{Function: functionCall{Name: "shell", Arguments: `{"command":"touch outside"}`}},
		{Function: functionCall{Name: "edit", Arguments: `{}`}},
	} {
		if _, err := f.execute(call, &workflowSet{}); err == nil {
			t.Fatal("fixture boundary bypassed")
		}
	}
	output, err := f.execute(toolCall{Function: functionCall{Name: "search", Arguments: `{"query":"setMessages"}`}}, &workflowSet{})
	if err != nil || !strings.Contains(output, "chat.ts:") {
		t.Fatalf("search failed: %q, %v", output, err)
	}
}
