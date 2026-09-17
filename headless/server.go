package headless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	rt "nekocode/runtime"
	"nekocode/util/version"
)

type turn struct {
	input                     userInput
	runID                     rt.RunID
	cancel                    context.CancelFunc
	started                   time.Time
	messageID, kind, lastText string
	text                      strings.Builder
	committedText             strings.Builder
	committedIDs              []string
	tools                     map[toolKey]*toolState
	wireIDs                   map[string]bool
	metrics                   *rt.MetricsSnapshot
	summary                   *rt.RunSummary
	denials                   []map[string]any
}

type pendingRequest struct {
	kind, id string
	approval *rt.ApprovalView
}

type server struct {
	backend            Backend
	conn               *transport
	options            Options
	cwd, sessionID     string
	active             *turn
	queue              []userInput
	queueBytes         int
	pending            map[string]pendingRequest
	seen               map[string]bool
	eof, initialized   bool
	shuttingDown       bool
	cancelInteractions bool
}

// Serve owns and closes both streams on return, including cancellation. Their
// Close methods must unblock pending Read/Write calls. The backend is borrowed
// exclusively for this connection and remains owned by the caller.
func Serve(ctx context.Context, in io.ReadCloser, out io.WriteCloser, backend Backend, cwd string, options Options) (err error) {
	if in == nil || out == nil {
		return errors.New("headless: nil stream")
	}
	defer in.Close()
	defer out.Close()
	if backend == nil {
		return errors.New("headless: nil backend")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := &server{backend: backend, options: options, cwd: cwd, pending: make(map[string]pendingRequest), seen: make(map[string]bool)}
	s.conn = newTransport(ctx, in, out)
	defer func() {
		cancel()
		_ = in.Close()
		_ = out.Close()
		if s.active != nil {
			s.active.cancel()
			_ = backend.CancelRun(context.Background(), s.active.runID)
			_ = backend.WaitRun(context.Background(), s.active.runID)
		}
		s.conn.wg.Wait()
	}()
	if options.Resume != "" {
		err = backend.ResumeSession(options.Resume)
	} else {
		_, err = backend.NewSession()
	}
	if err == nil {
		s.sessionID = backend.CurrentSessionID()
		if s.sessionID == "" {
			err = errors.New("backend did not bind a session")
		}
	}
	if err == nil {
		err = s.serve(ctx)
	}
	if err != nil {
		// This is a connection failure, not a successful turn settlement.
		_ = s.send("system", map[string]any{"subtype": "error", "error": err.Error(), "fatal": true})
	}
	flushCtx, stopFlush := context.WithTimeout(ctx, 2*time.Second)
	defer stopFlush()
	return errors.Join(err, s.conn.flush(flushCtx))
}

func (s *server) serve(ctx context.Context) error {
	events, err := s.backend.Events(ctx, rt.EventFilter{Reliable: true})
	if err != nil {
		return err
	}
	reads := s.conn.reads
	if s.options.Prompt != "" {
		s.queue = append(s.queue, userInput{id: uuid.NewString(), text: s.options.Prompt})
		s.queueBytes = len(s.options.Prompt)
		s.eof = true
		reads = nil
	}
	for {
		if s.active == nil && len(s.queue) != 0 {
			input := s.queue[0]
			s.queue[0] = userInput{}
			s.queue = s.queue[1:]
			s.queueBytes -= len(input.text)
			if err := s.start(ctx, input); err != nil {
				return err
			}
			if s.active == nil {
				continue
			}
		}
		if (s.eof || s.shuttingDown) && s.active == nil && len(s.queue) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-s.conn.errors:
			return err
		case incoming := <-reads:
			if errors.Is(incoming.err, io.EOF) {
				s.eof = true
				reads = nil
				if err := s.rejectPending(ctx); err != nil {
					return err
				}
				continue
			}
			if incoming.err != nil {
				return incoming.err
			}
			if err := s.receive(ctx, incoming.frame); err != nil {
				return err
			}
		case event, ok := <-events:
			if !ok {
				return errors.New("reliable runtime event stream closed (overflow or backend shutdown)")
			}
			if s.active == nil || event.RunID != s.active.runID {
				continue
			}
			if err := s.event(ctx, event); err != nil {
				return err
			}
		}
	}
}

func (s *server) start(ctx context.Context, input userInput) error {
	if s.backend.CurrentSessionID() != s.sessionID {
		return errors.New("backend session changed outside stream-json")
	}
	t := &turn{input: input, started: time.Now(), tools: make(map[toolKey]*toolState), wireIDs: make(map[string]bool)}
	s.active = t
	turnCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	// Snapshot configuration before starting the runner, then publish init with
	// the allocated ID before consuming any of its buffered events.
	init := map[string]any{
		"subtype": "init", "protocol_version": ProtocolVersion,
		"nekocode_version": version.Version, "cwd": s.cwd,
		"model": s.backend.CurrentModel().Model, "permissionMode": s.backend.PermissionMode(),
		"capabilities": s.capabilities(), "server": s.metadata(ctx),
	}
	id, err := s.backend.StartRun(turnCtx, rt.Input{Source: rt.SourceRef{Kind: "headless", ID: input.id}, Text: input.text})
	t.runID = id
	if sendErr := s.sendTurn("system", init); sendErr != nil {
		return sendErr
	}
	if err != nil {
		cancel()
		if err := s.result(rt.RunResult{Error: err.Error()}, "error_during_execution"); err != nil {
			return err
		}
		s.active = nil
		return nil
	}
	return nil
}

func (s *server) send(kind string, fields map[string]any) error {
	fields["type"] = kind
	fields["uuid"] = uuid.NewString()
	fields["session_id"] = s.sessionID
	return s.conn.send(fields)
}

// sendTurn tags a frame with the identity of the turn it describes. Connection
// and input-lifecycle frames must use send instead: attributing a queued or
// rejected input to the running turn would misroute host correlation, whose
// documented key for those frames is accepted_uuid/rejected_uuid.
func (s *server) sendTurn(kind string, fields map[string]any) error {
	if s.active != nil {
		if _, ok := fields["user_message_uuid"]; !ok {
			fields["user_message_uuid"] = s.active.input.id
		}
		if s.active.runID != "" {
			fields["run_id"] = s.active.runID
		}
	}
	return s.send(kind, fields)
}

func (s *server) result(result rt.RunResult, subtype string) error {
	fields := map[string]any{
		"result":      result.Output,
		"duration_ms": time.Since(s.active.started).Milliseconds(),
	}
	reason := "completed"
	if subtype == "error_cancelled" {
		reason = "cancelled"
	} else if subtype != "success" {
		reason = "execution_error"
	}
	if s.active.summary != nil {
		fields["step_count"] = s.active.summary.StepCount
		if subtype == "success" {
			reason = s.active.summary.StopReason
		}
	}
	fields["stop_reason"] = reason
	switch reason {
	case "step_limit":
		subtype = "error_step_limit"
	case "cancelled":
		subtype = "error_cancelled"
	case "execution_error":
		subtype = "error_during_execution"
	}
	fields["subtype"], fields["is_error"] = subtype, subtype != "success"
	if s.active.denials == nil {
		s.active.denials = []map[string]any{}
	}
	fields["permission_denials"] = s.active.denials
	if result.Error != "" {
		fields["errors"] = []string{result.Error}
	}
	// Expose the runtime's own units and scope, without fabricating prices or
	// claiming Anthropic cache accounting semantics.
	if s.active.metrics != nil {
		fields["nekocode_usage"] = s.active.metrics
	}
	return s.sendTurn("result", fields)
}

func capabilities() []string {
	return []string{"text", "fifo_turns", "interrupt", "cancel_queued", "can_use_tool", "nekocode_question", "synthetic_content_stream", "subagent_output", "run_summary", "permission_denials"}
}

// clearQueue returns the discarded input IDs in FIFO order for the control ACK.
func (s *server) clearQueue() []string {
	ids := make([]string, 0, len(s.queue))
	for _, input := range s.queue {
		ids = append(ids, input.id)
	}
	s.queue = nil
	s.queueBytes = 0
	return ids
}

func (s *server) receive(ctx context.Context, f frame) error {
	if len(f.UUID) > 256 || len(f.RequestID) > 256 || len(f.Response.RequestID) > 256 || len(f.SessionID) > 256 {
		return s.send("system", map[string]any{"subtype": "error", "error": "IDs must not exceed 256 bytes"})
	}
	switch f.Type {
	case "keep_alive":
		return nil
	case "user":
		if s.shuttingDown {
			return s.send("system", map[string]any{"subtype": "error", "error": "connection is shutting down", "rejected_uuid": f.UUID})
		}
		input, err := f.user()
		if err == nil && f.SessionID != "" && f.SessionID != s.sessionID {
			err = errors.New("user session_id does not match the bound session")
		}
		if err != nil {
			return s.send("system", map[string]any{"subtype": "error", "error": err.Error(), "rejected_uuid": f.UUID})
		}
		if input.id == "" {
			input.id = uuid.NewString()
		}
		if s.seen[input.id] {
			return s.send("system", map[string]any{"subtype": "input_duplicate", "user_message_uuid": input.id})
		}
		if len(s.queue) >= maxQueuedTurns || s.queueBytes+len(input.text) > 16<<20 {
			return s.send("system", map[string]any{"subtype": "error", "error": "input queue full", "rejected_uuid": input.id})
		}
		if len(s.seen) >= 10000 {
			return errors.New("connection input ID limit reached; reconnect")
		}
		s.seen[input.id] = true
		s.queue = append(s.queue, input)
		s.queueBytes += len(input.text)
		return s.send("system", map[string]any{"subtype": "input_queued", "accepted_uuid": input.id})
	case "control_request":
		return s.control(ctx, f)
	case "control_response":
		return s.resolve(ctx, f)
	case "control_cancel_request":
		// Hosts may cancel their own inbound requests. Those are synchronous
		// here; this must never resolve an agent-originated permission request.
		return nil
	default:
		return s.send("system", map[string]any{"subtype": "error", "error": fmt.Sprintf("unsupported frame type %q", f.Type)})
	}
}
