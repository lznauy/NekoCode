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
	ownerID            core.RunID
	view               core.QuestionView
	reply              chan protocol.QuestionReply
	runID              core.RunID
	requestedPublished bool
	resolvedReply      protocol.QuestionReply
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
	return b.RegisterForRun(b.currentRunID(), req)
}

func (b *QuestionBroker) RegisterForRun(runID core.RunID, req protocol.QuestionRequest) func() protocol.QuestionReply {
	return b.RegisterScoped(runID, runID, req)
}

// RegisterScoped separates cancellation ownership from the public event run ID.
func (b *QuestionBroker) RegisterScoped(ownerID, runID core.RunID, req protocol.QuestionRequest) func() protocol.QuestionReply {
	id := fmt.Sprintf("q_%d", atomic.AddUint64(&b.nextID, 1))
	rec := &questionRecord{
		ownerID: ownerID,
		reply:   make(chan protocol.QuestionReply, 1),
		runID:   runID,
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
	viewCopy := cloneQuestionView(rec.view)
	b.mu.Unlock()

	b.publish(rec.runID, core.EventQuestionRequested, viewCopy)
	// Resolutions during requested callbacks are published after every requested
	// observer returns. Only one side owns terminal publication and the reply.
	b.mu.Lock()
	rec.requestedPublished = true
	resolved := rec.view.Status != core.QuestionPending
	b.mu.Unlock()
	if resolved {
		b.publishResolved([]*questionRecord{rec})
	}

	return func() protocol.QuestionReply {
		return <-rec.reply
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
	rec.resolvedReply = cloneQuestionReply(reply)
	publishResolved := rec.requestedPublished
	b.mu.Unlock()

	if publishResolved {
		b.publishResolved([]*questionRecord{rec})
	}
	return nil
}

func (b *QuestionBroker) Pending() []core.QuestionView {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]core.QuestionView, 0, len(b.pending))
	for _, rec := range b.pending {
		out = append(out, cloneQuestionView(rec.view))
	}
	return out
}

func (b *QuestionBroker) RejectAll() {
	b.rejectAll(false, "")
}

func (b *QuestionBroker) Close() {
	b.rejectAll(true, "")
}

func (b *QuestionBroker) rejectAll(closeBroker bool, onlyRun core.RunID) {
	b.mu.Lock()
	if closeBroker {
		b.closed = true
	}
	var publishedRecords []*questionRecord
	for id, rec := range b.pending {
		if onlyRun != "" && rec.ownerID != onlyRun {
			continue
		}
		resolvedAt := time.Now()
		rec.view.ResolvedAt = &resolvedAt
		rec.view.Status = core.QuestionRejected
		rec.resolvedReply = protocol.QuestionReply{Rejected: true}
		if rec.requestedPublished {
			publishedRecords = append(publishedRecords, rec)
		}
		delete(b.pending, id)
	}
	b.mu.Unlock()

	b.publishResolved(publishedRecords)
}

// Commit every terminal projection before releasing any worker, then allow
// ordinary observers to reenter the broker and consume the committed replies.
func (b *QuestionBroker) publishResolved(records []*questionRecord) {
	events := make([]core.Event, 0, len(records))
	for _, rec := range records {
		events = append(events, core.Event{RunID: rec.runID, Type: core.EventQuestionResolved, Source: b.source, Payload: cloneQuestionView(rec.view)})
	}
	b.eventBus.PublishCommitted(events, func() {
		for _, rec := range records {
			rec.reply <- rec.resolvedReply
		}
	})
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

func cloneQuestionView(view core.QuestionView) core.QuestionView {
	view.Questions = cloneQuestions(view.Questions)
	return view
}

func cloneQuestionReply(reply protocol.QuestionReply) protocol.QuestionReply {
	reply.Answers = append([][]string(nil), reply.Answers...)
	for i := range reply.Answers {
		reply.Answers[i] = append([]string(nil), reply.Answers[i]...)
	}
	return reply
}

// RejectRun leaves other executions and background interactions untouched.
func (b *QuestionBroker) RejectRun(runID core.RunID) {
	if runID != "" {
		b.rejectAll(false, runID)
	}
}
