package broker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
	"nekocode/runtime/internal/eventbus"
)

type ApprovalBroker struct {
	mu       sync.Mutex
	nextID   uint64
	pending  map[string]*approvalRecord
	eventBus *eventbus.EventBus
	source   core.SourceRef
	runID    func() core.RunID
	closed   bool
}

type approvalRecord struct {
	view               core.ApprovalView
	reply              chan protocol.ConfirmReply
	runID              core.RunID
	requestedPublished bool
}

type approvalHashInput struct {
	ToolName string                    `json:"tool_name"`
	Kind     protocol.ConfirmKind      `json:"kind"`
	ArgsHash string                    `json:"args_hash"`
	Approval *protocol.ApprovalContext `json:"approval,omitempty"`
}

func NewApprovalBroker(eventBus *eventbus.EventBus, source core.SourceRef, runID func() core.RunID) *ApprovalBroker {
	return &ApprovalBroker{
		pending:  make(map[string]*approvalRecord),
		eventBus: eventBus,
		source:   source,
		runID:    runID,
	}
}

func (b *ApprovalBroker) Request(req protocol.ConfirmRequest) protocol.ConfirmReply {
	wait := b.Register(req)
	if wait == nil {
		return protocol.ConfirmReply{Cancelled: true}
	}
	return wait()
}

// Register publishes an approval and returns its blocking wait function.
func (b *ApprovalBroker) Register(req protocol.ConfirmRequest) func() protocol.ConfirmReply {
	id := fmt.Sprintf("apr_%d", atomic.AddUint64(&b.nextID, 1))
	now := time.Now()
	argsHash := stableHash(protocol.ToolInputArgs(req.Args))
	rec := &approvalRecord{
		reply: make(chan protocol.ConfirmReply, 1),
		runID: b.currentRunID(),
		view: core.ApprovalView{
			ID:           id,
			ToolName:     req.ToolName,
			CallID:       req.CallID,
			SubAgentID:   req.SubAgentID,
			Args:         cloneMap(protocol.ApprovalArgs(req.Args)),
			ArgsHash:     argsHash,
			ToolCallHash: stableHash(approvalHashInput{ToolName: req.ToolName, Kind: req.Kind, ArgsHash: argsHash, Approval: req.Approval}),
			Kind:         string(req.Kind),
			Status:       core.ApprovalPending,
			CreatedAt:    now,
			Source:       b.source,
			Approval:     req.Approval.Clone(),
		},
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.pending[id] = rec
	viewCopy := cloneApprovalView(rec.view)
	b.mu.Unlock()

	b.publish(rec.runID, core.EventApprovalRequested, viewCopy)
	// A resolver may commit while this notification invokes synchronous
	// observers. Defer its terminal event until every requested observer returns.
	b.mu.Lock()
	rec.requestedPublished = true
	resolved := rec.view.Status != core.ApprovalPending
	if resolved {
		viewCopy = cloneApprovalView(rec.view)
	}
	b.mu.Unlock()
	if resolved {
		b.publish(rec.runID, core.EventApprovalResolved, viewCopy)
	}

	return func() protocol.ConfirmReply {
		reply := <-rec.reply
		if !rec.view.Approval.CanRemember() {
			reply.Remember = false
		}
		return reply
	}
}

func (b *ApprovalBroker) Decide(id string, decision core.ApprovalDecision) error {
	b.mu.Lock()
	rec, ok := b.pending[id]
	if !ok {
		b.mu.Unlock()
		return fmt.Errorf("runtime: approval %s not pending", id)
	}
	if rec.view.Status != core.ApprovalPending {
		b.mu.Unlock()
		return fmt.Errorf("runtime: approval %s already resolved", id)
	}
	if decision.Remember && !rec.view.Approval.CanRemember() {
		b.mu.Unlock()
		return fmt.Errorf("runtime: approval %s only supports a one-time decision", id)
	}
	resolvedAt := time.Now()
	rec.view.ResolvedAt = &resolvedAt
	if decision.Allowed {
		rec.view.Status = core.ApprovalApproved
	} else {
		rec.view.Status = core.ApprovalRejected
	}
	delete(b.pending, id)
	viewCopy := cloneApprovalView(rec.view)
	publishResolved := rec.requestedPublished
	b.mu.Unlock()

	// Commit the reply before invoking synchronous observers: an observer may
	// wait for this request or reenter the broker to start another one.
	rec.reply <- decision.ConfirmReply()
	if publishResolved {
		b.publish(rec.runID, core.EventApprovalResolved, viewCopy)
	}
	return nil
}

func (b *ApprovalBroker) Pending() []core.ApprovalView {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]core.ApprovalView, 0, len(b.pending))
	for _, rec := range b.pending {
		out = append(out, cloneApprovalView(rec.view))
	}
	return out
}

func (b *ApprovalBroker) RejectAll() {
	b.rejectAll(false)
}

func (b *ApprovalBroker) Close() {
	b.rejectAll(true)
}

func (b *ApprovalBroker) rejectAll(closeBroker bool) {
	b.mu.Lock()
	if closeBroker {
		b.closed = true
	}
	records := make([]*approvalRecord, 0, len(b.pending))
	var publishedRecords []*approvalRecord
	for id, rec := range b.pending {
		resolvedAt := time.Now()
		rec.view.ResolvedAt = &resolvedAt
		rec.view.Status = core.ApprovalRejected
		records = append(records, rec)
		if rec.requestedPublished {
			publishedRecords = append(publishedRecords, rec)
		}
		delete(b.pending, id)
	}
	b.mu.Unlock()

	for _, rec := range records {
		rec.reply <- protocol.ConfirmReply{Cancelled: true}
	}
	for _, rec := range publishedRecords {
		b.publish(rec.runID, core.EventApprovalResolved, cloneApprovalView(rec.view))
	}
}

func cloneApprovalView(view core.ApprovalView) core.ApprovalView {
	view.Args = cloneMap(view.Args)
	view.Metadata = cloneMap(view.Metadata)
	view.Approval = view.Approval.Clone()
	return view
}

func (b *ApprovalBroker) publish(runID core.RunID, typ core.EventType, payload any) {
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

func (b *ApprovalBroker) currentRunID() core.RunID {
	if b.runID == nil {
		return ""
	}
	return b.runID()
}

func stableHash(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
