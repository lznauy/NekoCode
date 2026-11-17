package standard

import (
	"testing"

	"nekocode/mode"
	controlruntime "nekocode/runtime"
)

func TestSessionRunPreservesDraftAndReusesCommandContext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if err := rt.SetInteractionMode(mode.Plan); err != nil {
		t.Fatal(err)
	}
	main := rt.CurrentSessionID()
	session := ""
	for i := 0; i < 3; i++ {
		id, selected, err := rt.StartRunInSession(t.Context(), session, controlruntime.Input{Text: "/help"})
		if err != nil {
			t.Fatal(err)
		}
		if session != "" && session != selected {
			t.Fatal("context session changed")
		}
		session = selected
		if err := rt.WaitRun(t.Context(), id); err != nil {
			t.Fatal(err)
		}
		if rt.CurrentSessionID() != main || rt.InteractionMode().Selected != mode.Plan {
			t.Fatalf("foreground draft changed: id=%s want=%s mode=%s", rt.CurrentSessionID(), main, rt.InteractionMode().Selected)
		}
	}
	if err := rt.DeleteInactiveSession(session); err != nil {
		t.Fatal(err)
	}
}
