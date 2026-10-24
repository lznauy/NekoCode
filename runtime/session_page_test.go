package runtime

import "testing"

func TestSessionMessagePageFallbackAndValidation(t *testing.T) {
	all := []DisplayMessage{{Content: "first"}, {Content: "second"}, {Content: "third"}}
	r := New(&testBot{}, ServiceGroups{Sessions: SessionsServices{
		CurrentSessionID: func() string { return "session" }, ListSessions: func() []SessionMeta { return nil }, SessionMessages: func() []DisplayMessage { return all },
	}})
	defer r.Close()
	page, err := r.SessionMessagesPage(1, 1)
	if err != nil || page.Total != 3 || page.NextOffset != 2 || !page.HasMore || page.Messages[0].Content != "second" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	for _, args := range [][2]int{{-1, 1}, {4, 1}, {0, 0}, {0, 1001}} {
		if _, err := r.SessionMessagesPage(args[0], args[1]); err == nil {
			t.Fatalf("invalid page accepted: %v", args)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SessionMessagesPage(0, 1); err == nil {
		t.Fatal("closed runtime returned a page")
	}
}
