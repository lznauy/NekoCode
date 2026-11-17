package tui

import (
	tea "charm.land/bubbletea/v2"
	rt "nekocode/runtime"
	"reflect"
	"testing"
)

func TestQuestionKeyKeepsPromptUntilAllQuestionsAnswered(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	var reply *rt.QuestionReply
	m.QuestionBar.SetRequest(&rt.QuestionRequest{Questions: []rt.QuestionItem{
		{Question: "First?", Options: []rt.QuestionOption{{Label: "A"}, {Label: "B"}}},
		{Question: "Second?", Options: []rt.QuestionOption{{Label: "X"}, {Label: "Y"}}},
	}}, func(got rt.QuestionReply) { reply = &got })
	m.state = stateQuestioning
	m.preConfirmState = stateProcessing
	m.QuestionBar.Move(1)
	m.handleQuestionKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.state != stateQuestioning || reply != nil {
		t.Fatalf("left question mode before second answer: state=%v reply=%+v", m.state, reply)
	}
	m.handleQuestionKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.state != stateProcessing || reply == nil || !reflect.DeepEqual(reply.Answers, [][]string{{"B"}, {"X"}}) {
		t.Fatalf("final reply=%+v state=%v", reply, m.state)
	}
}
