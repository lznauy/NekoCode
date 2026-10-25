package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"nekocode/logger"
	"nekocode/protocol"
	internalconnectors "nekocode/runtime/internal/connectors"
	"nekocode/runtime/internal/eventbus"
	"nekocode/runtime/internal/recording"
	"nekocode/runtime/internal/runstore"
)

const recordedRunRestoreLimit = runstore.DefaultLimit

type Connector = internalconnectors.Connector
type ConnectorFactory = internalconnectors.ConnectorFactory

func (r *Runtime) Events(ctx context.Context, filter EventFilter) (<-chan Event, error) {
	return r.events.Subscribe(ctx, filter)
}

// Notify publishes a background system message (no run attached) so
// transports can surface out-of-band outcomes, e.g. an MCP authorization
// flow finishing while the user was away.
func (r *Runtime) Notify(content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return
	}
	r.events.Publish(Event{
		Type:    EventSystemMessage,
		Source:  SourceRef{Kind: "runtime"},
		Payload: MessagePayload{Content: content},
	})
}

func (r *Runtime) ReplayEvents(ctx context.Context, filter EventFilter) (<-chan Event, error) {
	if filter.RunID != "" {
		// Archived runs are completed and immutable. Serialize with pruning
		// while reading, then seed only this subscriber, preserving large
		// terminal results without growing or displacing shared history.
		r.recordingMu.Lock()
		defer r.recordingMu.Unlock()
		r.mu.Lock()
		run, ok := r.archivedRuns[filter.RunID]
		r.mu.Unlock()
		if ok {
			recent := make([]Event, 0)
			bytes := 0
			if err := recording.WalkRecordedEventFile(run.Path, func(ev Event) {
				ev = eventbus.NormalizeRecordedEvent(ev)
				if !eventbus.Matches(filter, ev) {
					return
				}
				if ev.Type == EventCompaction {
					if p, ok := ev.Payload.(CompactionPayload); ok && p.Status == protocol.CompactionDelta {
						return
					}
				}
				recent = append(recent, ev)
				bytes += eventbus.EstimateEventBytes(ev)
				// Keep at least the final record even when it alone exceeds
				// the budget, just as a single oversized history page does.
				for len(recent) > 1 && (len(recent) > 1000 || bytes > 8<<20) {
					bytes -= eventbus.EstimateEventBytes(recent[0])
					recent[0] = Event{}
					recent = recent[1:]
				}
			}); err != nil {
				if len(r.events.History(EventFilter{RunID: filter.RunID})) > 0 {
					return r.events.SubscribeReplay(ctx, filter)
				}
				return nil, err
			}
			if len(recent) == 0 {
				return r.events.SubscribeReplay(ctx, filter)
			}
			return r.events.SubscribeReplayEvents(ctx, filter, recent)
		}
	}
	return r.events.SubscribeReplay(ctx, filter)
}

func (r *Runtime) RegisterConnector(name string, factory ConnectorFactory) {
	if len(r.connectors.View().Connectors) == 0 {
		r.registerConnectorCommands()
	}
	r.connectors.Register(name, factory)
}

// DeliverConnectorFile sends a local file to the paired chats of the named
// connector, or to every running file-capable connector when name is empty.
// It returns the connector names the file was sent through. It backs the
// deliver_file tool.
func (r *Runtime) DeliverConnectorFile(ctx context.Context, name, path string) ([]string, error) {
	if err := r.ensureOpen(); err != nil {
		return nil, err
	}
	return r.connectors.DeliverFile(ctx, name, path)
}

func (r *Runtime) Connect(ctx context.Context, name string, args []string) (string, error) {
	if err := r.ensureOpen(); err != nil {
		return "", err
	}
	return r.connectors.Handle(ctx, append([]string{name}, args...))
}

func (r *Runtime) Disconnect(name string) (string, error) {
	if err := r.ensureOpen(); err != nil {
		return "", err
	}
	return r.connectors.Disconnect(name)
}

func (r *Runtime) ConnectView() ConnectView {
	return r.connectors.View()
}

// ReportConnectorStatus publishes the only event type connectors may inject.
func (r *Runtime) ReportConnectorStatus(payload ConnectorStatusPayload) {
	if payload.Name == "" {
		return
	}
	r.events.Publish(Event{
		Type:    EventConnectorStatus,
		Source:  SourceRef{Kind: payload.Name},
		Payload: payload,
	})
}

// EnableDefaultEventRecording persists run events under the NekoCode data
// directory and restores previously recorded run history.
func (r *Runtime) EnableDefaultEventRecording() error {
	return r.EnableEventRecording(recording.DefaultBaseDir())
}

// immediateRestoredRuns is how many of the newest recorded runs are decoded
// synchronously during startup so /last /status answer instantly. Older runs
// stay on disk until an explicit lookup or replay requests their contents.
const immediateRestoredRuns = 1

// BindRecordedSession moves provisional recordings into the real session, or
// selects a new session for future events. A failed bind preserves its source.
func (r *Runtime) BindRecordedSession(sessionID string) {
	if r == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	r.mu.Lock()
	recorder := r.recorder
	r.mu.Unlock()
	if recorder == nil {
		return
	}
	if err := recorder.BindSession(sessionID); err != nil {
		logger.Log("runtime: bind recorded session %s: %v", sessionID, err)
	}
}

func (r *Runtime) beginRecordedRun(runID RunID) (bool, error) {
	r.mu.Lock()
	recorder := r.recorder
	r.mu.Unlock()
	if recorder == nil {
		return false, nil
	}
	id := recorder.SessionID()
	if r.services.Sessions.CurrentSessionID != nil {
		var err error
		id, err = r.recordingSessionID()
		if err != nil {
			return false, err
		}
	}
	return id == "", recorder.BeginRun(runID, id)
}

func (r *Runtime) recordingSessionID() (id string, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("runtime: read recording session: %v", v)
		}
	}()
	return r.services.Sessions.CurrentSessionID(), nil
}

func (r *Runtime) bindRecordedRun(runID RunID) {
	id, err := r.recordingSessionID()
	if err == nil && id != "" {
		err = r.recorder.BindRunSession(runID, id)
	}
	if err != nil {
		logger.Log("runtime: bind recorded run %s: %v", runID, err)
	}
}

func (r *Runtime) releaseRecordedRun(runID RunID) {
	r.mu.Lock()
	recorder := r.recorder
	r.mu.Unlock()
	if recorder != nil {
		path := filepath.Join(recorder.RunDir(runID), "events.jsonl")
		recorder.EndRun(runID)
		r.mu.Lock()
		if r.archivedRuns == nil {
			r.archivedRuns = make(map[RunID]recording.RecordedRun)
		}
		r.archivedRuns[runID] = recording.RecordedRun{Path: path, Sequence: r.nextRun}
		for len(r.archivedRuns) > recordedRunRestoreLimit {
			var oldest RunID
			var sequence uint64 = ^uint64(0)
			for id, run := range r.archivedRuns {
				if run.Sequence < sequence {
					oldest, sequence = id, run.Sequence
				}
			}
			delete(r.archivedRuns, oldest)
		}
		r.mu.Unlock()
	}
}

// EnableEventRecording persists run events under baseDir and restores
// previously recorded run history.
func (r *Runtime) EnableEventRecording(baseDir string) error {
	r.recordingMu.Lock()
	defer r.recordingMu.Unlock()
	r.mu.Lock()
	closed := r.closed
	configured := r.recorder != nil
	r.mu.Unlock()
	if closed {
		return protocolError(ErrorClosed, "enable_event_recording", "closed")
	}
	if configured {
		return nil
	}
	if strings.TrimSpace(baseDir) == "" {
		return fmt.Errorf("runtime: empty event recording directory")
	}
	runs, err := recording.ScanRecordedRuns(baseDir, recordedRunRestoreLimit)
	if err != nil {
		return fmt.Errorf("runtime: load recorded events: %w", err)
	}

	// Advance the run sequence synchronously from directory names alone —
	// sub-millisecond, and it must complete before the first new run starts
	// or restored run IDs would collide with fresh ones.
	r.advanceRunSequenceForRuns(runs)

	// Only the newest run is decoded eagerly; the rest are path metadata.
	r.mu.Lock()
	r.archivedRuns = make(map[RunID]recording.RecordedRun, len(runs))
	for _, run := range runs {
		r.archivedRuns[RunID(filepath.Base(filepath.Dir(run.Path)))] = run
	}
	r.mu.Unlock()
	immediate := runs
	if len(immediate) > immediateRestoredRuns {
		immediate = immediate[len(immediate)-immediateRestoredRuns:]
	}
	for _, run := range immediate {
		if err := recording.WalkRecordedEventFile(run.Path, func(ev Event) { r.runs.Record(ev); r.events.ImportHistory([]Event{ev}) }); err != nil {
			logger.Log("runtime: restore recent run: %v", err)
		}
	}

	recorder, err := recording.NewEventRecorder(baseDir)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = recorder.Close()
		return protocolError(ErrorClosed, "enable_event_recording", "closed")
	}
	r.recorder = recorder
	r.mu.Unlock()
	r.events.AddOrderedObserver(recorder.Record)

	go r.pruneRecordedRuns(baseDir)
	return nil
}

func (r *Runtime) pruneRecordedRuns(baseDir string) {
	r.recordingMu.Lock()
	defer r.recordingMu.Unlock()
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return
	}
	if err := recording.PruneRecordedRuns(baseDir, recording.RetentionAge, recording.RetentionBytes); err != nil {
		logger.Log("runtime: prune recorded runs: %v", err)
	}
	r.recorder.StartRetention()
}

// advanceRunSequenceForRuns pushes the next run counter past every recorded
// run ID parsed from directory names, without decoding any event file.
func (r *Runtime) advanceRunSequenceForRuns(runs []recording.RecordedRun) {
	var maxID uint64
	for _, run := range runs {
		if run.Sequence > maxID {
			maxID = run.Sequence
		}
	}
	if maxID == 0 {
		return
	}
	r.mu.Lock()
	if maxID > r.nextRun {
		r.nextRun = maxID
	}
	r.mu.Unlock()
}
