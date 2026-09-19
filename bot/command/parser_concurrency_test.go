package command

import (
	"context"
	"fmt"
	"testing"
)

func TestSkillCommandsPublishAtomicallyWithConcurrentReaders(t *testing.T) {
	h := New(Deps{})
	sets := [][]SkillRegistration{
		{{Name: "first-a"}, {Name: "first-b"}},
		{{Name: "second-a"}, {Name: "second-b"}},
	}
	h.RegisterSkills(sets[0])
	done := make(chan error, 1)
	go func() {
		for range 1000 {
			menu, _ := h.Menu(context.Background(), "$")
			if len(menu.Items) != 2 {
				done <- fmt.Errorf("partial skill publication: %+v", menu.Items)
				return
			}
			if menu.Items[0].Value != "$first-a" && menu.Items[0].Value != "$second-a" {
				done <- fmt.Errorf("unexpected menu: %+v", menu.Items)
				return
			}
			if menu.Items[0].Value[:len(menu.Items[0].Value)-1] != menu.Items[1].Value[:len(menu.Items[1].Value)-1] {
				done <- fmt.Errorf("mixed skill generations: %+v", menu.Items)
				return
			}
			// /help re-enters the parser; its callback must not hold a lock.
			if _, ok := h.Parser().Execute(context.Background(), h.Parser().Parse("/help")); !ok {
				done <- fmt.Errorf("help unavailable during refresh")
				return
			}
		}
		done <- nil
	}()
	for i := range 1000 {
		h.RegisterSkills(sets[i%len(sets)])
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
