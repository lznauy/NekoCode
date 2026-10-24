package core

import (
	"fmt"
	"sync"
	"testing"
)

func TestHistoryReadsStayWithinSessionDuringSwitch(t *testing.T) {
	b := newPersistTestBot(t)
	var ids []string
	for index, name := range []string{"session-A", "session-B"} {
		if _, err := b.NewSession(); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 32-index*16; i++ {
			b.ctxMgr.Add("user", name)
		}
		if err := b.saveSession(); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, b.CurrentSessionID())
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	failures := make(chan error, 2)
	wg.Go(func() {
		<-start
		for i := 0; i < 300; i++ {
			if err := b.ResumeSession(ids[i%2]); err != nil {
				failures <- err
				return
			}
		}
	})
	wg.Go(func() {
		<-start
		for i := 0; i < 3000; i++ {
			page, err := b.HistoryPage(0, 32, 1<<20)
			if err != nil {
				failures <- err
				return
			}
			if len(page.Messages) != 32 && len(page.Messages) != 16 {
				failures <- fmt.Errorf("history length = %d", len(page.Messages))
				return
			}
			name := page.Messages[0].Content
			for _, msg := range page.Messages {
				if msg.Content != name {
					failures <- fmt.Errorf("mixed history: %q and %q", name, msg.Content)
					return
				}
			}
			snapshot := b.Conversation()
			if len(snapshot.Transcript) != 32 && len(snapshot.Transcript) != 16 {
				failures <- fmt.Errorf("conversation length = %d", len(snapshot.Transcript))
				return
			}
			name = snapshot.Transcript[0].Content
			for _, msg := range snapshot.Transcript {
				if msg.Content != name {
					failures <- fmt.Errorf("mixed conversation: %q and %q", name, msg.Content)
					return
				}
			}
		}
	})
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
