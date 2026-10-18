package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	controlruntime "nekocode/runtime"
)

type Runtime interface {
	controlruntime.Interaction
	controlruntime.ModelReader
	controlruntime.ObservationReader
	CurrentRun() (controlruntime.RunSnapshot, bool)
	LookupRun(controlruntime.RunID) (controlruntime.RunSnapshot, bool)
	Runs(limit int) []controlruntime.RunSnapshot
	Status() controlruntime.RuntimeStatus
	Capabilities() controlruntime.CapabilityManifest
	ConnectView() controlruntime.ConnectView
	Connect(context.Context, string, []string) (string, error)
	Disconnect(string) (string, error)
	Metrics() controlruntime.MetricsSnapshot
	ContextSnapshot() controlruntime.ContextSnapshot
	MemoryView(controlruntime.MemoryScope) controlruntime.MemoryView
	ListSessions() []controlruntime.SessionMeta
	SessionMessages() []controlruntime.DisplayMessage
	CommandCatalog() []string
	ReplayEvents(context.Context, controlruntime.EventFilter) (<-chan controlruntime.Event, error)
}

type Server struct {
	rt Runtime
}

func New(rt Runtime) *Server {
	return &Server{rt: rt}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("GET /capabilities", s.handleCapabilities)
	// Transport-neutral observation surface for external applications.
	mux.HandleFunc("GET /observe/overview", s.handleObserveOverview)
	mux.HandleFunc("GET /observe/runs", s.handleObserveRuns)
	mux.HandleFunc("GET /observe/runs/{runID}", s.handleObserveRun)
	mux.HandleFunc("GET /observe/metrics", s.handleObservePrometheus)
	mux.HandleFunc("GET /observe/models", s.handleObserveModels)
	mux.HandleFunc("GET /observe/tools", s.handleObserveTools)
	mux.HandleFunc("GET /observe/sources", s.handleObserveSources)
	mux.HandleFunc("GET /runs", s.handleRuns)
	mux.HandleFunc("GET /runs/current", s.handleCurrentRun)
	mux.HandleFunc("GET /connect", s.handleConnect)
	mux.HandleFunc("POST /connect/{name}", s.handleConnectCommand)
	mux.HandleFunc("POST /disconnect/{name}", s.handleDisconnectCommand)
	mux.HandleFunc("GET /model", s.handleModel)
	mux.HandleFunc("GET /commands", s.handleCommands)
	mux.HandleFunc("GET /commands/menu", s.handleCommandMenu)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /context", s.handleContext)
	mux.HandleFunc("GET /memory", s.handleMemory)
	mux.HandleFunc("GET /sessions", s.handleSessions)
	mux.HandleFunc("GET /sessions/current/messages", s.handleSessionMessages)
	mux.HandleFunc("POST /input", s.handleInput)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /runs/{runID}", s.handleRun)
	mux.HandleFunc("POST /runs/{runID}/abort", s.handleAbort)
	mux.HandleFunc("POST /approvals/{approvalID}", s.handleApproval)
	mux.HandleFunc("POST /questions/{questionID}", s.handleQuestion)
	return mux
}

type submitRequest struct {
	Text   string                    `json:"text"`
	Source *controlruntime.SourceRef `json:"source,omitempty"`
	Sender *controlruntime.SenderRef `json:"sender,omitempty"`
	// Images are optional local image attachments; each Label must appear in
	// Text for the runtime's attachment envelope to accept it.
	Images []controlruntime.ImageAttachment `json:"images,omitempty"`
}

type submitResponse struct {
	RunID controlruntime.RunID `json:"run_id"`
}

type approvalRequest struct {
	Allowed  bool `json:"allowed"`
	Remember bool `json:"remember,omitempty"`
	// Deprecated compatibility input; unified approvals ignore it.
	AllowWithPermission bool `json:"allow_with_permission,omitempty"`
}

type questionRequest struct {
	Answers  [][]string `json:"answers,omitempty"`
	Rejected bool       `json:"rejected,omitempty"`
}

type connectRequest struct {
	Args []string `json:"args,omitempty"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	status := s.rt.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       status.State != controlruntime.RuntimeClosed,
		"protocol": controlruntime.ProtocolVersion,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.rt.Status())
}

func (s *Server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.rt.Capabilities())
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	writeJSON(w, http.StatusOK, map[string]any{"runs": s.rt.Runs(limit)})
}

func (s *Server) handleCurrentRun(w http.ResponseWriter, _ *http.Request) {
	run, ok := s.rt.CurrentRun()
	if !ok {
		writeError(w, http.StatusNotFound, "current run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleConnect(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Capabilities().Connectors {
		writeError(w, http.StatusNotImplemented, "connector capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.rt.ConnectView())
}

func (s *Server) handleConnectCommand(w http.ResponseWriter, r *http.Request) {
	if !s.rt.Capabilities().Connectors {
		writeError(w, http.StatusNotImplemented, "connector capability unavailable")
		return
	}
	var req connectRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := s.rt.Connect(r.Context(), r.PathValue("name"), req.Args)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": resp})
}

func (s *Server) handleDisconnectCommand(w http.ResponseWriter, r *http.Request) {
	if !s.rt.Capabilities().Connectors {
		writeError(w, http.StatusNotImplemented, "connector capability unavailable")
		return
	}
	resp, err := s.rt.Disconnect(r.PathValue("name"))
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": resp})
}

func (s *Server) handleModel(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Capabilities().Models {
		writeError(w, http.StatusNotImplemented, "model capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.rt.CurrentModel())
}

func (s *Server) handleCommands(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Capabilities().Commands {
		writeError(w, http.StatusNotImplemented, "command capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": s.rt.CommandCatalog()})
}

func (s *Server) handleCommandMenu(w http.ResponseWriter, r *http.Request) {
	if !s.rt.Capabilities().Commands {
		writeError(w, http.StatusNotImplemented, "command capability unavailable")
		return
	}
	menu, ok := s.rt.CommandMenu(r.Context(), r.URL.Query().Get("input"))
	if !ok {
		writeError(w, http.StatusNotFound, "command menu not found")
		return
	}
	writeJSON(w, http.StatusOK, menu)
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Capabilities().Metrics {
		writeError(w, http.StatusNotImplemented, "metrics capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.rt.Metrics())
}

func (s *Server) handleContext(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Capabilities().Context {
		writeError(w, http.StatusNotImplemented, "context capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.rt.ContextSnapshot())
}

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	if !s.rt.Capabilities().Context {
		writeError(w, http.StatusNotImplemented, "context capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.rt.MemoryView(controlruntime.MemoryScope(r.URL.Query().Get("scope"))))
}

func (s *Server) handleSessions(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Capabilities().Sessions {
		writeError(w, http.StatusNotImplemented, "session capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.rt.ListSessions()})
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Capabilities().Sessions {
		writeError(w, http.StatusNotImplemented, "session capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": s.rt.SessionMessages()})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	runID := controlruntime.RunID(r.PathValue("runID"))
	run, ok := s.rt.LookupRun(runID)
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	source := controlruntime.SourceRef{Kind: "http"}
	if req.Source != nil {
		source = *req.Source
		if source.Kind == "" {
			source.Kind = "http"
		}
	}
	var sender controlruntime.SenderRef
	if req.Sender != nil {
		sender = *req.Sender
	}
	runID, err := s.rt.StartRun(context.WithoutCancel(r.Context()), controlruntime.Input{
		Source: source,
		Sender: sender,
		Text:   req.Text,
		Images: req.Images,
	})
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, submitResponse{RunID: runID})
}

func (s *Server) handleAbort(w http.ResponseWriter, r *http.Request) {
	runID := controlruntime.RunID(r.PathValue("runID"))
	if err := s.rt.CancelRun(r.Context(), runID); err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request) {
	var req approvalRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err := s.rt.DecideApproval(r.Context(), r.PathValue("approvalID"), controlruntime.ApprovalDecision{
		Allowed:             req.Allowed,
		Remember:            req.Remember,
		AllowWithPermission: req.AllowWithPermission,
	})
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleQuestion(w http.ResponseWriter, r *http.Request) {
	var req questionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err := s.rt.AnswerQuestion(r.Context(), r.PathValue("questionID"), controlruntime.QuestionReply{
		Answers:  req.Answers,
		Rejected: req.Rejected,
	})
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	filter := controlruntime.EventFilter{
		RunID: controlruntime.RunID(r.URL.Query().Get("run_id")),
		After: parseEventCursor(r.URL.Query().Get("after"), r.Header.Get("Last-Event-ID")),
	}
	var events <-chan controlruntime.Event
	var err error
	if r.URL.Query().Get("replay") == "1" || filter.After > 0 {
		events, err = s.rt.ReplayEvents(r.Context(), filter)
	} else {
		events, err = s.rt.Events(r.Context(), filter)
	}
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(w, ev); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func decodeJSON(r *http.Request, out any) error {
	defer func() { _ = r.Body.Close() }()
	if err := decodeSingleJSON(r.Body, out); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func decodeOptionalJSON(r *http.Request, out any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	defer func() { _ = r.Body.Close() }()
	if err := decodeSingleJSON(r.Body, out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func decodeSingleJSON(body io.Reader, out any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return errors.New("multiple json values")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeRuntimeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	body := map[string]any{"error": err.Error()}
	var protocolErr *controlruntime.ProtocolError
	if errors.As(err, &protocolErr) {
		body["code"] = protocolErr.Code
		switch protocolErr.Code {
		case controlruntime.ErrorClosed:
			status = http.StatusServiceUnavailable
		case controlruntime.ErrorBusy, controlruntime.ErrorConflict:
			status = http.StatusConflict
		case controlruntime.ErrorNotFound:
			status = http.StatusNotFound
		case controlruntime.ErrorUnsupported:
			status = http.StatusNotImplemented
		}
	}
	writeJSON(w, status, body)
}

func writeSSE(w http.ResponseWriter, ev controlruntime.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if ev.ID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", ev.ID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", ev.Type); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func parseLimit(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	if n > 200 {
		return 200
	}
	return n
}

func parseEventCursor(raw, lastEventID string) uint64 {
	if raw == "" {
		raw = strings.TrimPrefix(lastEventID, "evt_")
	}
	cursor, _ := strconv.ParseUint(raw, 10, 64)
	return cursor
}
