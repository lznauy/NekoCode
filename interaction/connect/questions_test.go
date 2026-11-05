package connect

import (
	"testing"

	controlruntime "nekocode/runtime"
)

func questionView(id string, multiple bool) controlruntime.QuestionView {
	return controlruntime.QuestionView{
		ID: id,
		Questions: []controlruntime.QuestionItem{{
			Question: "pick",
			Options: []controlruntime.QuestionOption{
				{Label: "Alpha"},
				{Label: "Beta"},
			},
			Multiple: multiple,
		}},
	}
}

func TestQuestionTrackerAddRemoveLast(t *testing.T) {
	tr := NewQuestionTracker()
	tr.Add(questionView("q_1", false))
	tr.Add(questionView("q_2", false))
	if tr.LastID() != "q_2" {
		t.Fatalf("LastID = %q, want q_2", tr.LastID())
	}
	tr.Remove("q_2")
	if tr.LastID() != "" {
		t.Fatalf("after removing last, LastID = %q", tr.LastID())
	}
	if _, ok := tr.View("q_1"); !ok {
		t.Fatal("q_1 should still be pending")
	}
}

func TestQuestionTrackerBuildReplyByNumberAndLabel(t *testing.T) {
	tr := NewQuestionTracker()
	tr.Add(questionView("q_1", false))

	reply, id, err := tr.BuildReply("", "2") // empty id → latest
	if err != nil || id != "q_1" {
		t.Fatalf("BuildReply: id=%q err=%v", id, err)
	}
	if len(reply.Answers) != 1 || reply.Answers[0][0] != "Beta" {
		t.Fatalf("reply = %+v", reply)
	}

	reply, _, err = tr.BuildReply("q_1", "alp") // label prefix
	if err != nil || reply.Answers[0][0] != "Alpha" {
		t.Fatalf("reply = %+v, err=%v", reply, err)
	}
}

func TestQuestionTrackerMultiSelect(t *testing.T) {
	tr := NewQuestionTracker()
	tr.Add(questionView("q_1", true))

	reply, _, err := tr.BuildReply("q_1", "Alpha, Beta")
	if err != nil || len(reply.Answers[0]) != 2 {
		t.Fatalf("reply = %+v, err=%v", reply, err)
	}

	reply, _, err = tr.BuildMultiOptionReply("q_1", []int{0, 1})
	if err != nil || len(reply.Answers[0]) != 2 {
		t.Fatalf("multi option reply = %+v, err=%v", reply, err)
	}
	if _, _, err := tr.BuildMultiOptionReply("q_1", nil); err == nil {
		t.Fatal("empty multi-select must error")
	}
}

func TestQuestionTrackerUnknownQuestion(t *testing.T) {
	tr := NewQuestionTracker()
	if _, _, err := tr.BuildReply("", "hi"); err == nil {
		t.Fatal("answer with no pending question must error")
	}
	if _, err := tr.Reject("q_nope"); err == nil {
		t.Fatal("reject of unknown question must error")
	}
}

func TestParseAnswerCommand(t *testing.T) {
	id, answer := ParseAnswerCommand("/answer q_12 hello world")
	if id != "q_12" || answer != "hello world" {
		t.Fatalf("id=%q answer=%q", id, answer)
	}
	id, answer = ParseAnswerCommand("/answer just text")
	if id != "" || answer != "just text" {
		t.Fatalf("id=%q answer=%q", id, answer)
	}
	if _, answer = ParseAnswerCommand("/answer"); answer != "" {
		t.Fatalf("bare /answer: answer=%q", answer)
	}
}

func TestMatchQuestionOptionExactAndAmbiguous(t *testing.T) {
	options := []controlruntime.QuestionOption{{Label: "Delete all"}, {Label: "Delete"}, {Label: "Keep"}}
	for _, tc := range []struct {
		token, want string
		ok          bool
	}{
		{"delete", "Delete", true}, {"del", "", false}, {"kee", "Keep", true}, {"1", "Delete all", true},
	} {
		got, ok := matchQuestionOption(options, tc.token)
		if got != tc.want || ok != tc.ok {
			t.Errorf("match(%q) = (%q, %v), want (%q, %v)", tc.token, got, ok, tc.want, tc.ok)
		}
	}
}
