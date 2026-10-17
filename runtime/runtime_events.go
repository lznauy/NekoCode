package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"nekocode/logger"
	internalconnectors "nekocode/runtime/internal/connectors"
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
// synchronously during startup so /last /status answer instantly. The long
// tail streams in from a background goroutine — decoding all of them inline
// cost ~5s of startup on histories of a few hundred runs.
const immediateRestoredRuns = 10

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

	// The newest runs restore synchronously into the run store (the data
	// source of /last /diff /status); the event-bus history is imported in
	// one piece — either synchronously when everything fit in the immediate
	// slice, or once the background tail finishes, keeping its ordering
	// correct.
	var immediate []recording.RecordedRun
	if len(runs) > immediateRestoredRuns {
		immediate, runs = runs[len(runs)-immediateRestoredRuns:], runs[:len(runs)-immediateRestoredRuns]
	} else {
		immediate, runs = runs, nil
	}
	if events := recording.LoadRecordedEventFiles(immediate); len(events) > 0 {
		for _, ev := range events {
			r.runs.Record(ev)
		}
		if len(runs) == 0 {
			// Small histories have no tail to stream — the synchronous batch
			// is the complete restore, so the event-bus history comes from
			// it directly.
			r.events.ImportHistory(events)
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
	r.events.AddObserver(recorder.Record)

	if len(runs) > 0 {
		tail, immediateRuns := runs, immediate
		go func() {
			// Enforce retention before the tail restore — pruning stale
			// batches is disk work that must never block startup, and doing
			// it first frees the space the restore would otherwise compete
			// with. The active batch's fresh mtime keeps it safe.
			if err := recording.PruneRecordedRuns(baseDir, recording.RetentionAge, recording.RetentionBytes); err != nil {
				logger.Log("runtime: prune recorded runs: %v", err)
			}
			// Two distinct sets: the run store only needs the tail — the
			// immediate slice was already Recorded synchronously, and Record
			// accumulates without dedup, so replaying it would double every
			// event count and concatenate duplicated outputs. The event-bus
			// history, by contrast, needs the complete restore (tail +
			// immediate) exactly once and in order — parallel process batches
			// overlap in time, so the two time-ordered slices are re-sorted
			// after merging.
			tailEvents := recording.LoadRecordedEventFiles(tail)
			immediateEvents := recording.LoadRecordedEventFiles(immediateRuns)
			r.mu.Lock()
			closed := r.closed
			r.mu.Unlock()
			if closed {
				return
			}
			for _, ev := range tailEvents {
				r.runs.Record(ev)
			}
			events := append(tailEvents, immediateEvents...)
			if len(events) == 0 {
				return
			}
			sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
			r.events.ImportHistory(events)
		}()
	}
	return nil
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
