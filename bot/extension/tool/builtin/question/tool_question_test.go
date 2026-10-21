package question

import (
	"context"
	"strings"
	"testing"

	"nekocode/protocol"
)

func TestQuestionToolExecutesWithAnswers(t *testing.T) {
	tool := NewTool()
	params := tool.Parameters()
	if len(params) != 1 || params[0].Items == nil || params[0].Items.Properties["options"].Items == nil {
		t.Fatalf("question schema does not describe nested questions: %+v", params)
	}
	tool.SetQuestionFunc(func(req protocol.QuestionRequest) protocol.QuestionReply {
		if len(req.Questions) != 1 {
			t.Fatalf("expected one question, got %d", len(req.Questions))
		}
		if req.Questions[0].Question != "Pick a mode" {
			t.Fatalf("unexpected question: %q", req.Questions[0].Question)
		}
		return protocol.QuestionReply{Answers: [][]string{{"Fast"}}}
	})

	out, err := tool.Execute(context.Background(), map[string]any{
		"questions": []any{
			map[string]any{
				"header":   "Mode",
				"question": "Pick a mode",
				"options": []any{
					map[string]any{"label": "Fast", "description": "Less detail"},
					map[string]any{"label": "Deep", "description": "More detail"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(out.Render(), "Pick a mode: Fast") {
		t.Fatalf("unexpected output: %s", out.Render())
	}
}

func TestQuestionToolReject(t *testing.T) {
	tool := NewTool()
	tool.SetQuestionFunc(func(protocol.QuestionRequest) protocol.QuestionReply {
		return protocol.QuestionReply{Rejected: true}
	})
	_, err := tool.Execute(context.Background(), map[string]any{
		"questions": []any{map[string]any{"question": "Continue?"}},
	})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected rejected error, got %v", err)
	}
}
