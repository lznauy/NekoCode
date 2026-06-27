package question

import (
	"context"
	"strings"
	"testing"

	"nekocode/common"
)

func TestQuestionToolExecutesWithAnswers(t *testing.T) {
	tool := NewTool()
	tool.SetQuestionFunc(func(req common.QuestionRequest) common.QuestionReply {
		if len(req.Questions) != 1 {
			t.Fatalf("expected one question, got %d", len(req.Questions))
		}
		if req.Questions[0].Question != "Pick a mode" {
			t.Fatalf("unexpected question: %q", req.Questions[0].Question)
		}
		return common.QuestionReply{Answers: [][]string{{"Fast"}}}
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
	if !strings.Contains(out, "Pick a mode: Fast") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestQuestionToolReject(t *testing.T) {
	tool := NewTool()
	tool.SetQuestionFunc(func(common.QuestionRequest) common.QuestionReply {
		return common.QuestionReply{Rejected: true}
	})
	_, err := tool.Execute(context.Background(), map[string]any{
		"questions": []any{map[string]any{"question": "Continue?"}},
	})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected rejected error, got %v", err)
	}
}
