package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
	events, err := recording.LoadRecentRecordedEvents(baseDir, recordedRunRestoreLimit)
	if err != nil {
		return fmt.Errorf("runtime: load recorded events: %w", err)
	}
	if len(events) > 0 {
		for _, ev := range events {
			r.runs.Record(ev)
		}
		r.events.ImportHistory(events)
		r.advanceRunSequence(events)
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
	return nil
}

func (r *Runtime) advanceRunSequence(events []Event) {
	var maxID uint64
	for _, ev := range events {
		n, ok := parseRunSequence(ev.RunID)
		if ok && n > maxID {
			maxID = n
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

func parseRunSequence(runID RunID) (uint64, bool) {
	raw := strings.TrimPrefix(string(runID), "run_")
	if raw == string(runID) || raw == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	return n, err == nil
}
