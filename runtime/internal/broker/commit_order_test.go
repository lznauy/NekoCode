package broker

import (
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
	"nekocode/runtime/internal/eventbus"
)

func TestResolvedEventCommitsBeforeWorkerReply(t *testing.T) {
	for _, question := range []bool{false, true} {
		name := "approval"
		if question {
			name = "question"
		}
		for _, reject := range []bool{false, true} {
			action := "resolve"
			if reject {
				action = "reject"
			}
			t.Run(name+"/"+action, func(t *testing.T) {
				bus := eventbus.NewEventBus()
				entered, release := make(chan struct{}), make(chan struct{})
				replied, finished := make(chan struct{}), make(chan struct{})
				typ := core.EventApprovalResolved
				var wait func()
				var resolve func()
				if question {
					typ = core.EventQuestionResolved
					b := NewQuestionBroker(bus, core.SourceRef{}, nil)
					w := b.Register(protocol.QuestionRequest{})
					id := b.Pending()[0].ID
					wait = func() { w() }
					resolve = func() {
						if reject {
							b.RejectAll()
						} else if err := b.Answer(id, protocol.QuestionReply{}); err != nil {
							t.Error(err)
						}
					}
				} else {
					b := NewApprovalBroker(bus, core.SourceRef{}, nil)
					w := b.Register(protocol.ConfirmRequest{})
					id := b.Pending()[0].ID
					wait = func() { w() }
					resolve = func() {
						if reject {
							b.RejectAll()
						} else if err := b.Decide(id, core.ApprovalDecision{Allowed: true}); err != nil {
							t.Error(err)
						}
					}
				}
				bus.AddOrderedObserver(func(ev core.Event) {
					if ev.Type == typ {
						close(entered)
						<-release
					}
				})
				go func() { wait(); close(replied) }()
				go func() { resolve(); close(finished) }()
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("resolution did not reach commit barrier")
				}
				select {
				case <-replied:
					t.Error("worker resumed before resolved projection committed")
				case <-time.After(100 * time.Millisecond):
				}
				close(release)
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Fatal("resolution stalled")
				}
				select {
				case <-replied:
				case <-time.After(time.Second):
					t.Fatal("reply was not released")
				}
			})
		}
	}
}
