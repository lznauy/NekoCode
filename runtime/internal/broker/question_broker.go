package broker

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
	"nekocode/runtime/internal/eventbus"
)

type QuestionBroker struct {
	mu       sync.Mutex
	nextID   uint64
	pending  map[string]*questionRecord
	eventBus *eventbus.EventBus
	source   core.SourceRef
	runID    func() core.RunID
	closed   bool
}

type questionRecord struct {
	view  core.QuestionView
	reply chan protocol.QuestionReply
	runID core.RunID
}

func NewQuestionBroker(eventBus *eventbus.EventBus, source core.SourceRef, runID func() core.RunID) *QuestionBroker {
	return &QuestionBroker{
		pending:  make(map[string]*questionRecord),
		eventBus: eventBus,
		source:   source,
		runID:    runID,
	}
}

func (b *QuestionBroker) Request(req protocol.QuestionRequest) protocol.QuestionReply {
	wait := b.Register(req)
	if wait == nil {
		return protocol.QuestionReply{Rejected: true}
	}
	return wait()
}

// Register publishes a question and returns its blocking wait function.
func (b *QuestionBroker) Register(req protocol.QuestionRequest) func() protocol.QuestionReply {
	id := fmt.Sprintf("q_%d", atomic.AddUint64(&b.nextID, 1))
	rec := &questionRecord{
		reply: make(chan protocol.QuestionReply, 1),
		runID: b.currentRunID(),
		view: core.QuestionView{
			ID:         id,
			SubAgentID: req.SubAgentID,
			Questions:  cloneQuestions(req.Questions),
			Status:     core.QuestionPending,
			CreatedAt:  time.Now(),
			Source:     b.source,
		},
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.pending[id] = rec
	viewCopy := rec.view
	b.mu.Unlock()

	b.publish(rec.runID, core.EventQuestionRequested, viewCopy)
	return func() protocol.QuestionReply {
		reply := <-rec.reply

		b.mu.Lock()
		if current, ok := b.pending[id]; ok && current.view.Status == core.QuestionPending {
			resolvedAt := time.Now()
			current.view.ResolvedAt = &resolvedAt
			if reply.Rejected {
				current.view.Status = core.QuestionRejected
			} else {
				current.view.Status = core.QuestionAnswered
			}
			delete(b.pending, id)
			b.publish(current.runID, core.EventQuestionResolved, current.view)
		}
		b.mu.Unlock()
		return reply
	}
}

func (b *QuestionBroker) Answer(id string, reply protocol.QuestionReply) error {
	b.mu.Lock()
	rec, ok := b.pending[id]
	if !ok {
		b.mu.Unlock()
		return fmt.Errorf("runtime: question %s not pending", id)
	}
	if rec.view.Status != core.QuestionPending {
		b.mu.Unlock()
		return fmt.Errorf("runtime: question %s already resolved", id)
	}
	resolvedAt := time.Now()
	rec.view.ResolvedAt = &resolvedAt
	if reply.Rejected {
		rec.view.Status = core.QuestionRejected
	} else {
		rec.view.Status = core.QuestionAnswered
	}
	delete(b.pending, id)
	viewCopy := rec.view
	b.publish(rec.runID, core.EventQuestionResolved, viewCopy)
	b.mu.Unlock()

	rec.reply <- reply
	return nil
}

func (b *QuestionBroker) Pending() []core.QuestionView {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]core.QuestionView, 0, len(b.pending))
	for _, rec := range b.pending {
		out = append(out, rec.view)
	}
	return out
}

func (b *QuestionBroker) RejectAll() {
	b.rejectAll(false)
}

func (b *QuestionBroker) Close() {
	b.rejectAll(true)
}

func (b *QuestionBroker) rejectAll(closeBroker bool) {
	b.mu.Lock()
	if closeBroker {
		b.closed = true
	}
	records := make([]*questionRecord, 0, len(b.pending))
	for id, rec := range b.pending {
		resolvedAt := time.Now()
		rec.view.ResolvedAt = &resolvedAt
		rec.view.Status = core.QuestionRejected
		records = append(records, rec)
		delete(b.pending, id)
	}
	b.mu.Unlock()

	for _, rec := range records {
		rec.reply <- protocol.QuestionReply{Rejected: true}
		b.publish(rec.runID, core.EventQuestionResolved, rec.view)
	}
}

func (b *QuestionBroker) publish(runID core.RunID, typ core.EventType, payload any) {
	if b.eventBus == nil {
		return
	}
	b.eventBus.Publish(core.Event{
		RunID:   runID,
		Type:    typ,
		Source:  b.source,
		Payload: payload,
	})
}

func (b *QuestionBroker) currentRunID() core.RunID {
	if b.runID == nil {
		return ""
	}
	return b.runID()
}
