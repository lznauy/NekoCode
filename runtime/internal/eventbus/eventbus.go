package eventbus

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

const defaultSubscriberBuffer = 128
const defaultSubscriberQueueLimit = 1000
const defaultEventHistoryLimit = 1000
const defaultEventHistoryBytes = 8 << 20

type EventBus struct {
	mu           sync.Mutex
	nextSubID    int
	nextEventID  uint64
	subscribers  map[int]*subscriber
	observers    []func(core.Event)
	observerWG   sync.WaitGroup
	history      []core.Event
	historySizes []int
	historyLimit int
	historyStart int
	historyCount int
	historyBytes int
	closed       bool
}

type subscriber struct {
	filter core.EventFilter
	out    chan core.Event
	wake   chan struct{}
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	queue  []core.Event
	failed bool
}

func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[int]*subscriber), historyLimit: defaultEventHistoryLimit}
}

func (b *EventBus) AddObserver(fn func(core.Event)) {
	if b == nil || fn == nil {
		return
	}
	b.mu.Lock()
	b.observers = append(b.observers, fn)
	b.mu.Unlock()
}

func (b *EventBus) Subscribe(ctx context.Context, filter core.EventFilter) (<-chan core.Event, error) {
	return b.subscribeHistory(ctx, filter, false, nil)
}

func (b *EventBus) SubscribeReplay(ctx context.Context, filter core.EventFilter) (<-chan core.Event, error) {
	return b.subscribeHistory(ctx, filter, true, nil)
}

// SubscribeReplayEvents seeds one subscriber without putting archived payloads
// in the shared history cache. The caller bounds the supplied recent window.
func (b *EventBus) SubscribeReplayEvents(ctx context.Context, filter core.EventFilter, events []core.Event) (<-chan core.Event, error) {
	return b.subscribeHistory(ctx, filter, true, events)
}

func (b *EventBus) subscribeHistory(ctx context.Context, filter core.EventFilter, replay bool, recorded []core.Event) (<-chan core.Event, error) {
	if b == nil {
		return nil, fmt.Errorf("runtime: nil event bus")
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, fmt.Errorf("runtime: event bus closed")
	}
	var history []core.Event
	if recorded != nil {
		for _, ev := range recorded {
			if ev.Version == "" {
				ev.Version = core.ProtocolVersion
			}
			if ev.Sequence == 0 {
				ev.Sequence, _ = parseEventSequence(ev.ID)
			}
			if !isCompactionDelta(ev) && eventMatches(filter, ev) {
				history = append(history, ev)
			}
		}
	} else if replay {
		history = b.historyLocked(filter)
	}
	b.nextSubID++
	id := b.nextSubID
	sub := &subscriber{
		filter: filter,
		out:    make(chan core.Event, defaultSubscriberBuffer),
		wake:   make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		queue:  history,
	}
	b.subscribers[id] = sub
	b.mu.Unlock()

	go func() {
		defer func() {
			close(sub.out)
			close(sub.done)
		}()
		defer func() {
			b.mu.Lock()
			if current, ok := b.subscribers[id]; ok && current == sub {
				delete(b.subscribers, id)
			}
			b.mu.Unlock()
		}()
		sub.run(ctx)
	}()

	return sub.out, nil
}

func (s *subscriber) enqueue(ev core.Event) {
	s.mu.Lock()
	if s.failed {
		s.mu.Unlock()
		return
	}
	if len(s.queue) >= defaultSubscriberQueueLimit {
		if s.filter.Reliable {
			s.failed = true
			s.queue = nil
			s.close()
			s.mu.Unlock()
			return
		}
		if !mustDeliverEvent(ev) {
			s.mu.Unlock()
			return
		}
		for i, queued := range s.queue {
			if mustDeliverEvent(queued) {
				continue
			}
			copy(s.queue[i:], s.queue[i+1:])
			s.queue[len(s.queue)-1] = core.Event{}
			s.queue = s.queue[:len(s.queue)-1]
			break
		}
	}
	s.queue = append(s.queue, ev)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *subscriber) next() (core.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return core.Event{}, false
	}
	ev := s.queue[0]
	s.queue[0] = core.Event{}
	if len(s.queue) == 1 {
		s.queue = nil
	} else {
		s.queue = s.queue[1:]
	}
	return ev, true
}

func (s *subscriber) run(ctx context.Context) {
	for {
		if ev, ok := s.next(); ok {
			select {
			case s.out <- ev:
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			}
			continue
		}
		select {
		case <-s.wake:
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		}
	}
}

func (s *subscriber) close() {
	s.once.Do(func() { close(s.stop) })
}

func (s *subscriber) wait() {
	<-s.done
}

func mustDeliverEvent(ev core.Event) bool {
	if ev.Type == core.EventCompaction {
		// Unknown payload shapes are not deltas and remain must-deliver, so a
		// future publisher change cannot silently drop terminal events.
		return !isCompactionDelta(ev)
	}
	switch ev.Type {
	case core.EventApprovalRequested,
		core.EventAssistantReset,
		core.EventApprovalResolved,
		core.EventQuestionRequested,
		core.EventQuestionResolved,
		core.EventRunSummary,
		core.EventRunDone,
		core.EventRunFailed,
		core.EventRunCancelled,
		core.EventSessionChanged:
		return true
	default:
		return false
	}
}

func isCompactionDelta(ev core.Event) bool {
	if ev.Type != core.EventCompaction {
		return false
	}
	p, ok := ev.Payload.(protocol.CompactionEvent)
	return ok && p.Status == protocol.CompactionDelta
}

func (b *EventBus) Publish(ev core.Event) core.Event {
	if b == nil {
		return ev
	}
	if ev.Sequence == 0 {
		ev.Sequence = atomic.AddUint64(&b.nextEventID, 1)
	}
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("evt_%d", ev.Sequence)
	}
	if ev.Version == "" {
		ev.Version = core.ProtocolVersion
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ev
	}
	// Compaction deltas are excluded from history: a long compaction streams
	// hundreds of them, which would evict the run/approval boundary events
	// late subscribers replay. The terminal event carries the full summary, so
	// replay does not depend on deltas.
	if !isCompactionDelta(ev) {
		b.appendHistoryLocked(ev)
	}
	for _, sub := range b.subscribers {
		if eventMatches(sub.filter, ev) {
			sub.enqueue(ev)
		}
	}
	observers := append([]func(core.Event){}, b.observers...)
	b.observerWG.Add(len(observers))
	b.mu.Unlock()

	for _, observer := range observers {
		func() {
			defer b.observerWG.Done()
			observer(ev)
		}()
	}
	return ev
}

func (b *EventBus) History(filter core.EventFilter) []core.Event {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.historyLocked(filter)
}

// appendHistoryLocked overwrites the oldest slot once the history is full.
func (b *EventBus) appendHistoryLocked(ev core.Event) {
	size := eventBytes(ev)
	if size > defaultEventHistoryBytes {
		return
	}
	if b.historyLimit <= 0 {
		b.history = append(b.history, ev)
		b.historyCount = len(b.history)
		return
	}
	if len(b.history) == 0 {
		b.history = make([]core.Event, b.historyLimit)
		b.historySizes = make([]int, b.historyLimit)
	}
	for b.historyCount > 0 && (b.historyCount == len(b.history) || b.historyBytes+size > defaultEventHistoryBytes) {
		b.historyBytes -= b.historySizes[b.historyStart]
		b.history[b.historyStart] = core.Event{}
		b.historySizes[b.historyStart] = 0
		b.historyStart = (b.historyStart + 1) % len(b.history)
		b.historyCount--
	}
	index := (b.historyStart + b.historyCount) % len(b.history)
	b.history[index] = ev
	b.historySizes[index] = size
	b.historyCount++
	b.historyBytes += size
}

func (b *EventBus) historyLocked(filter core.EventFilter) []core.Event {
	out := make([]core.Event, 0, b.historyCount)
	for i := 0; i < b.historyCount; i++ {
		ev := b.history[(b.historyStart+i)%len(b.history)]
		if eventMatches(filter, ev) {
			out = append(out, ev)
		}
	}
	return out
}

func (b *EventBus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subscribers := b.subscribers
	b.subscribers = nil
	b.observers = nil
	b.mu.Unlock()
	for _, sub := range subscribers {
		sub.close()
	}
	for _, sub := range subscribers {
		sub.wait()
	}
	b.observerWG.Wait()
}

func (b *EventBus) ImportHistory(events []core.Event) {
	if b == nil || len(events) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var maxID uint64
	for _, ev := range b.history {
		n := ev.Sequence
		if n == 0 {
			n, _ = parseEventSequence(ev.ID)
		}
		if n > maxID {
			maxID = n
		}
	}
	for i := range events {
		ev := &events[i]
		if ev.Version == "" {
			ev.Version = core.ProtocolVersion
		}
		if ev.Sequence == 0 {
			ev.Sequence, _ = parseEventSequence(ev.ID)
		}
		if ev.Sequence > maxID {
			maxID = ev.Sequence
		}
		if !isCompactionDelta(*ev) {
			b.appendHistoryLocked(*ev)
		}
	}
	for {
		old := atomic.LoadUint64(&b.nextEventID)
		if maxID <= old || atomic.CompareAndSwapUint64(&b.nextEventID, old, maxID) {
			break
		}
	}
}

func parseEventSequence(id string) (uint64, bool) {
	raw := strings.TrimPrefix(id, "evt_")
	if raw == id || raw == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	return n, err == nil
}

func eventMatches(filter core.EventFilter, ev core.Event) bool {
	if filter.After > 0 && ev.Sequence <= filter.After {
		return false
	}
	if filter.RunID != "" && filter.RunID != ev.RunID {
		return false
	}
	if len(filter.Types) > 0 {
		found := false
		for _, typ := range filter.Types {
			if typ == ev.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.Sources) > 0 {
		found := false
		for _, src := range filter.Sources {
			if src == ev.Source.Kind {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
