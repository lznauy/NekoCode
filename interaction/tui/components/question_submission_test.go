package components

import (
	rt "nekocode/runtime"
	"reflect"
	"testing"
)

func TestQuestionBarConfirmsEachQuestionSeparately(t *testing.T) {
	bar := NewQuestionBar(nil)
	var got *rt.QuestionReply
	bar.SetRequest(&rt.QuestionRequest{Questions: []rt.QuestionItem{
		{Question: "First?", Options: []rt.QuestionOption{{Label: "A"}, {Label: "B"}}},
		{Question: "Second?", Options: []rt.QuestionOption{{Label: "X"}, {Label: "Y"}}},
	}}, func(reply rt.QuestionReply) { got = &reply })
	bar.Move(1)
	bar.Submit()
	if got != nil {
		t.Fatalf("unanswered second question was submitted: %+v", got.Answers)
	}
	if bar.activeQ != 1 || bar.activeOpt != 0 {
		t.Fatalf("did not advance to next question: %d %d", bar.activeQ, bar.activeOpt)
	}
	bar.Submit()
	if got == nil || !reflect.DeepEqual(got.Answers, [][]string{{"B"}, {"X"}}) {
		t.Fatalf("answers=%+v", got)
	}
}

func TestQuestionBarEmptyCustomAnswerStaysOpen(t *testing.T) {
	bar := NewQuestionBar(nil)
	called := false
	bar.SetRequest(&rt.QuestionRequest{Questions: []rt.QuestionItem{{Question: "Explain?", Custom: true}}}, func(rt.QuestionReply) { called = true })
	bar.Submit()
	if called || bar.req == nil {
		t.Fatal("empty custom answer submitted")
	}
	bar.Type("actual answer")
	bar.Submit()
	if !called {
		t.Fatal("valid answer not submitted")
	}
}

func TestQuestionBarCustomAnswerReplacesSingleSelection(t *testing.T) {
	bar := NewQuestionBar(nil)
	var got rt.QuestionReply
	bar.SetRequest(&rt.QuestionRequest{Questions: []rt.QuestionItem{{Question: "Choose?", Options: []rt.QuestionOption{{Label: "A"}}, Custom: true}}}, func(reply rt.QuestionReply) { got = reply })
	bar.Toggle()
	bar.Move(1)
	bar.Type("custom")
	bar.Submit()
	if !reflect.DeepEqual(got.Answers, [][]string{{"custom"}}) {
		t.Fatalf("single choice returned multiple answers: %+v", got.Answers)
	}
}
