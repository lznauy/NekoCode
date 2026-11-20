// Package connect provides shared connector lifecycle, command, pairing,
// persistence, and event-stream primitives.
package connect

import (
	"context"
	"sync"

	controlruntime "nekocode/runtime"
)

// Base is the run-state machine shared by IM connectors: a detached run
// context (immune to caller cancellation), a generation guard against
// stale loops, and connector_status publishing. Channels compose it into
// their own Connector struct.
type Base struct {
	rt          controlruntime.ConnectorRuntime
	name        string
	displayName string

	mu         sync.Mutex
	cancel     context.CancelFunc
	running    bool
	generation int
}

// NewBase creates the run-state base for a connector. name is the machine
// name used in event sources and payloads ("telegram"), displayName the
// human name used in status messages ("Telegram").
func NewBase(rt controlruntime.ConnectorRuntime, name, displayName string) *Base {
	return &Base{rt: rt, name: name, displayName: displayName}
}

// IsRunning reports whether the connector has a live run.
func (b *Base) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// Start detaches from the caller's context (it may be a single HTTP request
// or a finished run whose cancellation would silently kill the loops),
// cancels any previous run, and returns the run context plus its
// generation. The caller passes the generation to MarkStopped when its
// loop exits.
func (b *Base) Start(callerCtx context.Context) (runCtx context.Context, generation int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running && b.cancel != nil {
		b.cancel()
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(callerCtx))
	b.cancel = cancel
	b.running = true
	b.generation++
	return runCtx, b.generation
}

// MarkStopped clears the running state when a loop exits on its own. The
// generation guard prevents a stale loop from clobbering a newer Start.
func (b *Base) MarkStopped(generation int) {
	b.mu.Lock()
	if b.generation != generation || !b.running {
		b.mu.Unlock()
		return
	}
	b.cancel()
	b.cancel = nil
	b.running = false
	b.mu.Unlock()
	b.publishStopped()
}

// Stop cancels the current run and publishes the stopped status.
func (b *Base) Stop() error {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.cancel = nil
	b.running = false
	b.mu.Unlock()
	b.publishStopped()
	return nil
}

func (b *Base) publishStopped() {
	b.PublishStatus("stopped", b.displayName+" connector stopped.")
}

// PublishStatus feeds a connector_status event back into the runtime so
// UIs can reflect the connector state.
func (b *Base) PublishStatus(status, message string) {
	if b.rt == nil {
		return
	}
	b.rt.ReportConnectorStatus(controlruntime.ConnectorStatusPayload{
		Name:    b.name,
		Status:  status,
		Message: message,
	})
}
