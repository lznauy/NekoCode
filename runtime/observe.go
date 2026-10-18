package runtime

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Observation is a transport-neutral, read-only projection of the runtime.
// Both the HTTP API (/observe/*) and the headless control surface
// (observe.*) serve these values, so every integration sees the same numbers.
//
// Caveat: run snapshots live in the runtime's bounded in-memory store, so the
// history window is the runtime's retention, not an unbounded log.
const (
	// ObserveWindow describes the bounded retained history, not a time span.
	ObserveWindow = "retained"
	// ObserveRunLimit caps run listings from external observers.
	ObserveRunLimit = 1000
)

// ObservedRunState is the coarse lifecycle bucket used by observers.
type ObservedRunState string

const (
	ObservedIdle      ObservedRunState = "idle"
	ObservedRunning   ObservedRunState = "running"
	ObservedWaiting   ObservedRunState = "waiting"
	ObservedDone      ObservedRunState = "done"
	ObservedFailed    ObservedRunState = "failed"
	ObservedCancelled ObservedRunState = "cancelled"
)

// ObservedRunSummary is the per-run projection used by dashboards.
type ObservedRunSummary struct {
	ID         RunID            `json:"id"`
	Status     RunStatus        `json:"status"`
	State      ObservedRunState `json:"state"`
	Source     SourceRef        `json:"source"`
	Phase      string           `json:"phase,omitempty"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt *time.Time       `json:"finished_at,omitempty"`
	DurationMS int64            `json:"duration_ms"`
	Tools      int              `json:"tools"`
	ToolErrors int              `json:"tool_errors"`
	SubAgents  int              `json:"subagents"`
	Approvals  int              `json:"approvals"`
	EventCount int              `json:"event_count"`
}

// RunStats aggregates the in-memory run window.
type RunStats struct {
	Total      int `json:"total"`
	Active     int `json:"active"`
	Done       int `json:"done"`
	Failed     int `json:"failed"`
	Cancelled  int `json:"cancelled"`
	Tools      int `json:"tools"`
	ToolErrors int `json:"tool_errors"`
}

// ObserveOverview is the landing payload shared by every integration.
//
// Metrics carries one MetricsSnapshot: Turn* fields describe the most recent
// turn, not a historical run.
type ObserveOverview struct {
	GeneratedAt  time.Time          `json:"generated_at"`
	Window       string             `json:"window"`
	Runtime      RuntimeStatus      `json:"runtime"`
	Metrics      MetricsSnapshot    `json:"metrics"`
	Context      ContextSnapshot    `json:"context"`
	Model        ModelSelection     `json:"model"`
	ModelCatalog []ModelOption      `json:"model_catalog,omitempty"`
	Capabilities CapabilityManifest `json:"capabilities"`
	Runs         RunStats           `json:"runs"`
	Sources      map[string]int     `json:"sources"`
}

// ToolActivity is one tool invocation without arguments or output.
type ToolActivity struct {
	CallID     string     `json:"call_id,omitempty"`
	Name       string     `json:"name"`
	Status     ToolStatus `json:"status"`
	IsError    bool       `json:"is_error,omitempty"`
	SubAgentID string     `json:"subagent_id,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMS int64      `json:"duration_ms"`
}

// ObservedRunDetail is one run with privacy-safe activity.
type ObservedRunDetail struct {
	Run       ObservedRunSummary `json:"run"`
	Tools     []ToolActivity     `json:"tools"`
	Todos     int                `json:"todos"`
	SubAgents []SubAgentView     `json:"subagents,omitempty"`
}

// ObservedRunFilter narrows RunSummaries for observers.
type ObservedRunFilter struct {
	Limit  int
	State  ObservedRunState
	Source string
}

// RunStateOf maps a run status onto its observer bucket.
func RunStateOf(status RunStatus) ObservedRunState {
	switch status {
	case RunDone:
		return ObservedDone
	case RunFailed:
		return ObservedFailed
	case RunCancelled:
		return ObservedCancelled
	case RunWaitingApproval, RunWaitingQuestion:
		return ObservedWaiting
	case RunRunning:
		return ObservedRunning
	default:
		return ObservedIdle
	}
}

// SummarizeRun projects one run snapshot for observers.
func SummarizeObservedRun(run RunSnapshot) ObservedRunSummary {
	out := ObservedRunSummary{
		ID: run.ID, Status: run.Status, State: RunStateOf(run.Status),
		Source: run.Source, Phase: run.Phase, StartedAt: run.StartedAt,
		FinishedAt: run.FinishedAt, SubAgents: len(run.SubAgents),
		Approvals: len(run.Approvals), EventCount: run.EventCount,
	}
	out.Tools = len(run.Tools)
	for _, tool := range run.Tools {
		if tool.IsError || tool.Status == ToolBlocked {
			out.ToolErrors++
		}
	}
	normalizeTimeline(&out, run)
	out.DurationMS = observedDuration(out)
	return out
}

// normalizeTimeline repairs restored runs whose recorded start is later than
// their end. Older recordings did not persist a start time, so the first
// restored event (the terminal one) became "started_at". UpdatedAt always
// tracks the newest event and is therefore the trustworthy end.
func normalizeTimeline(out *ObservedRunSummary, run RunSnapshot) {
	if out.FinishedAt == nil || !out.StartedAt.After(*out.FinishedAt) {
		return
	}
	if !run.UpdatedAt.IsZero() && !run.UpdatedAt.Before(out.StartedAt) {
		out.FinishedAt = &run.UpdatedAt
		return
	}
	// No consistent end is available: keep the start and report no finish.
	out.FinishedAt = nil
	out.DurationMS = 0
}

func observedDuration(out ObservedRunSummary) int64 {
	if out.FinishedAt != nil {
		return max(out.FinishedAt.Sub(out.StartedAt).Milliseconds(), 0)
	}
	if out.State == ObservedRunning || out.State == ObservedWaiting {
		return max(time.Since(out.StartedAt).Milliseconds(), 0)
	}
	return 0
}

// ObservedRuns returns compact run projections, newest first. Filtering
// happens before limiting: a caller asking for failures must not miss older
// failures just because the newest runs happened to succeed.
func (r *Runtime) ObservedRuns(filter ObservedRunFilter) []ObservedRunSummary {
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > ObserveRunLimit {
		limit = ObserveRunLimit
	}
	// Scan the whole retained window, then truncate after filtering.
	runs := r.Runs(ObserveRunLimit)
	out := make([]ObservedRunSummary, 0, min(limit, len(runs)))
	for _, run := range runs {
		summary := SummarizeObservedRun(run)
		if filter.State != "" && summary.State != filter.State {
			continue
		}
		if filter.Source != "" && summary.Source.Kind != filter.Source {
			continue
		}
		if len(out) >= limit {
			break
		}
		out = append(out, summary)
	}
	return out
}

// ObservedRun returns one run with privacy-safe activity.
func (r *Runtime) ObservedRun(runID RunID) (ObservedRunDetail, bool) {
	run, ok := r.LookupRun(runID)
	if !ok {
		return ObservedRunDetail{}, false
	}
	tools := make([]ToolActivity, 0, len(run.Tools))
	for _, tool := range run.Tools {
		item := ToolActivity{
			CallID: tool.CallID, Name: tool.Name, Status: tool.Status,
			IsError: tool.IsError, SubAgentID: tool.SubAgentID,
			StartedAt: tool.StartedAt, FinishedAt: tool.FinishedAt,
		}
		if tool.FinishedAt != nil {
			item.DurationMS = max(tool.FinishedAt.Sub(tool.StartedAt).Milliseconds(), 0)
		}
		tools = append(tools, item)
	}
	return ObservedRunDetail{
		Run: SummarizeObservedRun(run), Tools: tools,
		Todos: len(run.Todos), SubAgents: run.SubAgents,
	}, true
}

// observeRunScan is how many retained runs the aggregate views inspect. The
// store is bounded by the runtime, so this is a review window, not a cap on
// what exists.
const observeRunScan = ObserveRunLimit

// runStats folds a run listing into the counters both the overview and the
// Prometheus exposition report. One implementation guarantees the two surfaces
// cannot disagree.
func runStats(runs []ObservedRunSummary) (RunStats, map[string]int) {
	stats := RunStats{}
	sources := map[string]int{}
	for _, run := range runs {
		switch run.State {
		case ObservedRunning, ObservedWaiting:
			stats.Active++
		case ObservedDone:
			stats.Done++
		case ObservedFailed:
			stats.Failed++
		case ObservedCancelled:
			stats.Cancelled++
		}
		stats.Total++
		stats.Tools += run.Tools
		stats.ToolErrors += run.ToolErrors
		if run.Source.Kind != "" {
			sources[run.Source.Kind]++
		}
	}
	return stats, sources
}

// ObservedOverview aggregates the in-memory run window with the current
// runtime state. It is the single source behind /observe/overview and
// headless observe.overview.
func (r *Runtime) ObservedOverview() ObserveOverview {
	caps := r.Capabilities()
	overview := ObserveOverview{
		GeneratedAt: time.Now(), Window: ObserveWindow,
		Runtime: r.Status(), Capabilities: caps,
	}
	if caps.Metrics {
		overview.Metrics = r.Metrics()
	}
	if caps.Context {
		overview.Context = r.ContextSnapshot()
	}
	if caps.Models {
		overview.Model = r.CurrentModel()
	}
	if caps.ModelCatalog {
		overview.ModelCatalog, _ = r.ModelOptions()
	}
	// Runs and Sources describe the same retained window: a source histogram
	// over a different span than the totals would make them incomparable.
	overview.Runs, overview.Sources = runStats(r.ObservedRuns(ObservedRunFilter{Limit: observeRunScan}))
	return overview
}

// ObservedSourceKinds returns the distinct source kinds in the run window,
// ordered alphabetically. External UIs use it to build filter menus.
func (r *Runtime) ObservedSourceKinds() []string {
	seen := map[string]struct{}{}
	for _, run := range r.ObservedRuns(ObservedRunFilter{Limit: ObserveRunLimit}) {
		if run.Source.Kind != "" {
			seen[run.Source.Kind] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for kind := range seen {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

// ObservedToolCatalog returns the configured tool names, sorted.
func (r *Runtime) ObservedToolCatalog() []string {
	names := r.ToolNames()
	sort.Strings(names)
	return names
}

// Prometheus renders the observation state in the text exposition format.
// Gauges describe the current/recent turn; run counters are named *_window
// because they follow the in-memory retention.
func (r *Runtime) Prometheus() string {
	var b strings.Builder
	caps := r.Capabilities()
	status := r.Status()
	// Same fold as the JSON overview, so a scrape and an API read always agree.
	stats, _ := runStats(r.ObservedRuns(ObservedRunFilter{Limit: observeRunScan}))
	metric := func(name, help, typ string, value int64) {
		b.WriteString("# HELP ")
		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(help)
		b.WriteByte('\n')

		b.WriteString("# TYPE ")
		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(typ)
		b.WriteByte('\n')

		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatInt(value, 10))
		b.WriteByte('\n')
	}
	state := 0
	if status.State == RuntimeBusy {
		state = 1
	} else if status.State == RuntimeClosed {
		state = 2
	}
	metric("nekocode_runtime_state", "0=ready 1=busy 2=closed", "gauge", int64(state))
	metric("nekocode_runs_active", "Runs currently executing or waiting", "gauge", int64(stats.Active))
	metric("nekocode_runs_done_window", "Completed runs in the in-memory window", "gauge", int64(stats.Done))
	metric("nekocode_runs_failed_window", "Failed runs in the in-memory window", "gauge", int64(stats.Failed))
	metric("nekocode_runs_cancelled_window", "Cancelled runs in the in-memory window", "gauge", int64(stats.Cancelled))
	metric("nekocode_tools_window", "Tool invocations across retained runs", "gauge", int64(stats.Tools))
	metric("nekocode_tool_errors_window", "Tool invocations that failed or were blocked", "gauge", int64(stats.ToolErrors))
	if caps.Metrics {
		m := r.Metrics()
		metric("nekocode_turn_input_tokens", "Input tokens of the most recent turn", "gauge", int64(m.TurnInput))
		metric("nekocode_turn_cached_tokens", "Cached input tokens of the most recent turn", "gauge", int64(m.TurnCached))
		metric("nekocode_turn_new_tokens", "Uncached input tokens of the most recent turn", "gauge", int64(m.TurnNew))
		metric("nekocode_turn_output_tokens", "Output tokens of the most recent turn", "gauge", int64(m.TurnOutput))
		metric("nekocode_turn_reasoning_tokens", "Reasoning tokens of the most recent turn", "gauge", int64(m.TurnReasoning))
		metric("nekocode_context_tokens", "Tokens currently occupied in the context window", "gauge", int64(m.ContextTokens))
		metric("nekocode_context_budget", "Context window budget", "gauge", int64(m.ContextBudget))
		// Compactions are per session and drop when a session switch restores a
		// smaller count, so this must not be a counter.
		metric("nekocode_compactions_session", "Compactions performed in this session", "gauge", int64(m.CompactCount))
	}
	return b.String()
}
