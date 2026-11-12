package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	controlruntime "nekocode/runtime"
)

type taskRuntime interface {
	CurrentSessionID() string
	Subagents(controlruntime.SubagentFilter) (controlruntime.SubagentPage, error)
	SubagentHistory(string, string, int, int) (controlruntime.SubagentHistoryPage, error)
	SubagentAttempts(string, string, int, int) (controlruntime.SubagentAttemptPage, error)
	SubagentToolOutput(string, string, int, int64) (controlruntime.SubagentOutputPage, error)
	DeleteSubagent(context.Context, string, string) error
	SearchSubagentOutputs(context.Context, string, string, string, int, int) (controlruntime.TaskSearchPage, error)
	ExportSubagentOutputs(context.Context, string, string, io.Writer) error
	BackgroundTasks(string) ([]controlruntime.BackgroundTask, error)
	BackgroundUsage(string) (controlruntime.BackgroundUsage, error)
	BackgroundDemands(string) ([]controlruntime.BackgroundDemand, error)
	RetryBackgroundNotification(context.Context, string, string) error
	AppendBackgroundTask(context.Context, string, string, string) error
	CancelBackgroundTask(context.Context, string, string) error
	StopBackgroundTasks(context.Context, string) error
	ResumeBackgroundDemand(context.Context, string, controlruntime.RunID) error
	DiscardBackgroundDemand(context.Context, string, controlruntime.RunID) error
}

func (s *Server) taskRoutes(mux *http.ServeMux) {
	for _, route := range []string{"GET /tasks", "GET /tasks/{taskID}/history", "GET /tasks/{taskID}/attempts", "GET /tasks/{taskID}/output", "GET /tasks/{taskID}/search", "GET /tasks/{taskID}/export", "DELETE /tasks/{taskID}", "GET /background", "POST /background/stop", "POST /background/{taskID}/retry", "POST /background/{taskID}/append", "POST /background/{taskID}/cancel", "GET /demands", "POST /demands/{demandID}/resume", "POST /demands/{demandID}/discard"} {
		mux.HandleFunc(route, s.handleTasks)
	}
}
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	rt, ok := s.rt.(taskRuntime)
	if !ok {
		writeError(w, 501, "task management unavailable")
		return
	}
	q := r.URL.Query()
	parent, id := q.Get("session_id"), r.PathValue("taskID")
	if r.Method == "GET" && id == "" && parent == "" {
		parent = rt.CurrentSessionID()
	}
	// Task details and mutations bind an explicit session rather than the selected UI.
	if (r.Method != "GET" || id != "") && parent == "" {
		writeError(w, 400, "session_id is required")
		return
	}
	number := func(name string, defaultValue int) int {
		raw := q.Get(name)
		if raw == "" {
			return defaultValue
		}
		v, err := strconv.Atoi(raw)
		if err != nil {
			return -1
		}
		return v
	}
	var value any
	var err error
	switch {
	case r.Pattern == "GET /tasks":
		value, err = rt.Subagents(controlruntime.SubagentFilter{AllSessions: q.Get("all") == "1", SessionID: parent, Query: q.Get("query"), Status: q.Get("status"), Offset: number("offset", 0), Limit: number("limit", 50)})
	case r.Pattern == "GET /background":
		var tasks []controlruntime.BackgroundTask
		var usage controlruntime.BackgroundUsage
		tasks, err = rt.BackgroundTasks(parent)
		if err == nil {
			usage, err = rt.BackgroundUsage(parent)
		}
		value = map[string]any{"tasks": tasks, "usage": usage}
	case r.Pattern == "GET /demands":
		value, err = rt.BackgroundDemands(parent)
	case r.Pattern == "POST /demands/{demandID}/resume":
		err = rt.ResumeBackgroundDemand(r.Context(), parent, controlruntime.RunID(r.PathValue("demandID")))
	case r.Pattern == "POST /demands/{demandID}/discard":
		err = rt.DiscardBackgroundDemand(r.Context(), parent, controlruntime.RunID(r.PathValue("demandID")))
	case r.Pattern == "POST /background/stop":
		err = rt.StopBackgroundTasks(r.Context(), parent)
	case r.Pattern == "POST /background/{taskID}/retry":
		err = rt.RetryBackgroundNotification(r.Context(), parent, id)
	case r.Pattern == "POST /background/{taskID}/cancel":
		err = rt.CancelBackgroundTask(r.Context(), parent, id)
	case r.Pattern == "POST /background/{taskID}/append":
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var req struct {
			Text string `json:"text"`
		}
		if err = decodeJSON(r, &req); err == nil {
			err = rt.AppendBackgroundTask(r.Context(), parent, id, req.Text)
		}
	case r.Pattern == "DELETE /tasks/{taskID}":
		err = rt.DeleteSubagent(r.Context(), parent, id)
	case r.Pattern == "GET /tasks/{taskID}/history":
		value, err = rt.SubagentHistory(parent, id, number("before", 0), number("limit", 50))
	case r.Pattern == "GET /tasks/{taskID}/attempts":
		value, err = rt.SubagentAttempts(parent, id, number("before", 0), number("limit", 50))
	case r.Pattern == "GET /tasks/{taskID}/output":
		value, err = rt.SubagentToolOutput(parent, id, number("sequence", 0), int64(number("offset", 0)))
	case r.Pattern == "GET /tasks/{taskID}/search":
		value, err = rt.SearchSubagentOutputs(r.Context(), parent, id, q.Get("query"), number("after", 0), number("limit", 20))
	case r.Pattern == "GET /tasks/{taskID}/export":
		if parent == "" {
			writeError(w, 400, "session_id is required")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="task-output.txt"`)
		w.Header().Set("Trailer", "X-Export-Error")
		stream := &taskExportWriter{ResponseWriter: w}
		err = rt.ExportSubagentOutputs(r.Context(), parent, id, stream)
		if err != nil {
			if !stream.started {
				w.Header().Del("Content-Disposition")
				w.Header().Del("Trailer")
				writeRuntimeError(w, err)
			} else {
				w.Header().Set("X-Export-Error", fmt.Sprintf("incomplete: %v", err))
			}
		}
		return
	}
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	if value == nil {
		value = map[string]bool{"ok": true}
	}
	writeJSON(w, 200, value)
}

type taskExportWriter struct {
	http.ResponseWriter
	started bool
}

func (w *taskExportWriter) Write(p []byte) (int, error) {
	w.started = true
	return w.ResponseWriter.Write(p)
}
