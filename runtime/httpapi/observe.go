package httpapi

import (
	"net/http"
	"strings"

	controlruntime "nekocode/runtime"
)

// Observation endpoints are transport-neutral, read-only projections of the
// runtime. Any external application — an IM bridge, a web dashboard, a CI job,
// a Prometheus scrape — can consume them without parsing the headless frame
// protocol. The aggregation lives in the runtime package so headless hosts
// (observe.*) serve exactly the same numbers.
//
// Caveat: run snapshots live in the runtime's bounded in-memory store, so the
// history window is the runtime's retention, not an unbounded log.

func (s *Server) handleObserveOverview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.rt.ObservedOverview())
}

// handleObserveRuns returns compact run projections. Prompt text, tool
// arguments, tool output, and model output stay out of this view.
func (s *Server) handleObserveRuns(w http.ResponseWriter, r *http.Request) {
	filter := controlruntime.ObservedRunFilter{
		Limit:  parseLimit(r.URL.Query().Get("limit"), 100),
		State:  controlruntime.ObservedRunState(strings.TrimSpace(r.URL.Query().Get("state"))),
		Source: strings.TrimSpace(r.URL.Query().Get("source")),
	}
	runs := s.rt.ObservedRuns(filter)
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "count": len(runs)})
}

// handleObserveRun returns one run's summary plus privacy-safe activity:
// tool names/status/duration and subagent identities, never arguments or
// outputs.
func (s *Server) handleObserveRun(w http.ResponseWriter, r *http.Request) {
	detail, ok := s.rt.ObservedRun(controlruntime.RunID(r.PathValue("runID")))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleObservePrometheus renders the same state in the Prometheus text
// exposition format.
func (s *Server) handleObservePrometheus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(s.rt.Prometheus()))
}

// handleObserveModels lists selectable models so an external UI can offer the
// same choices the TUI does.
func (s *Server) handleObserveModels(w http.ResponseWriter, _ *http.Request) {
	caps := s.rt.Capabilities()
	if !caps.ModelCatalog {
		writeError(w, http.StatusNotImplemented, "model_catalog capability unavailable")
		return
	}
	models, active := s.rt.ModelOptions()
	writeJSON(w, http.StatusOK, map[string]any{"models": models, "active": active})
}

// handleObserveTools exposes the configured tool catalog (names only).
func (s *Server) handleObserveTools(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Capabilities().ToolCatalog {
		writeError(w, http.StatusNotImplemented, "tool_catalog capability unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": s.rt.ObservedToolCatalog()})
}

// handleObserveSources lists run source kinds for filter menus.
func (s *Server) handleObserveSources(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": s.rt.ObservedSourceKinds()})
}
