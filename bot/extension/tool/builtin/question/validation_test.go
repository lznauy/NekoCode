package question

import (
	"context"
	"nekocode/protocol"
	"strings"
	"testing"
)

func TestQuestionRejectsInvalidRequestsBeforeUI(t *testing.T) {
	for _, fields := range []map[string]any{
		{"custom": false}, {"custom": "false"}, {"multiple": "true"},
		{"options": "yes"}, {"options": []any{"yes"}},
		{"options": []any{map[string]any{"label": " "}}},
		{"options": []any{map[string]any{"label": "yes"}, map[string]any{"label": " yes "}}},
	} {
		tool := NewTool()
		tool.SetQuestionFunc(func(protocol.QuestionRequest) protocol.QuestionReply {
			t.Fatal("invalid question reached UI")
			return protocol.QuestionReply{}
		})
		fields["question"] = "Choose?"
		if _, err := tool.Execute(context.Background(), map[string]any{"questions": []any{fields}}); err == nil {
			t.Fatalf("invalid question accepted: %+v", fields)
		}
	}
}

func TestQuestionDoesNotTreatMissingAnswersAsDecisions(t *testing.T) {
	tool := NewTool()
	tool.SetQuestionFunc(func(protocol.QuestionRequest) protocol.QuestionReply {
		return protocol.QuestionReply{Answers: [][]string{{"yes"}, {}}}
	})
	out, err := tool.Execute(context.Background(), map[string]any{"questions": []any{map[string]any{"question": "First?"}, map[string]any{"question": "Second?"}}})
	if err == nil || !strings.Contains(out.Text, "First?: yes") || strings.Contains(out.Text, "Continue with these answers") {
		t.Fatalf("incomplete answers reported as success or lost evidence: %+v %v", out, err)
	}
}

func TestQuestionRejectsInvalidAnswers(t *testing.T) {
	for _, answers := range [][][]string{nil, {{" "}}, {{"yes", "no"}}, {{"unknown"}}, {{"yes"}, {"extra"}}} {
		tool := NewTool()
		tool.SetQuestionFunc(func(protocol.QuestionRequest) protocol.QuestionReply { return protocol.QuestionReply{Answers: answers} })
		out, err := tool.Execute(context.Background(), map[string]any{"questions": []any{map[string]any{"question": "Choose?", "custom": false, "options": []any{map[string]any{"label": "yes"}, map[string]any{"label": "no"}}}}})
		if err == nil || strings.Contains(out.Text, "Continue with these answers") {
			t.Fatalf("invalid reply accepted: %+v %v", answers, err)
		}
	}
}

func TestQuestionAcceptsFreeTextAndMultipleChoices(t *testing.T) {
	tool := NewTool()
	tool.SetQuestionFunc(func(protocol.QuestionRequest) protocol.QuestionReply {
		return protocol.QuestionReply{Answers: [][]string{{"custom response"}, {"yes", "no"}}}
	})
	out, err := tool.Execute(context.Background(), map[string]any{"questions": []any{map[string]any{"question": "Explain?"}, map[string]any{"question": "Choose?", "custom": false, "multiple": true, "options": []any{map[string]any{"label": "yes"}, map[string]any{"label": "no"}}}}})
	if err != nil || !strings.Contains(out.Text, "Continue with these answers") {
		t.Fatalf("valid reply rejected: %+v %v", out, err)
	}
}
