package recording

import (
	"bufio"
	"encoding/json"
	"fmt"
	"nekocode/logger"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekocode/runtime/internal/core"
	"nekocode/util/fs"
)

type EventRecorder struct {
	mu            sync.Mutex
	baseDir       string
	sessionID     string
	provisionalID string
	runs          map[core.RunID]*runRecording
	writeErr      error
	lastPrune     time.Time
}

type runRecording struct {
	dir         string
	provisional bool
	lock        *os.File
}

type recordedEvent struct {
	Version     string          `json:"version,omitempty"`
	ID          string          `json:"id"`
	Sequence    uint64          `json:"sequence,omitempty"`
	RunID       core.RunID      `json:"run_id,omitempty"`
	Type        core.EventType  `json:"type"`
	Source      core.SourceRef  `json:"source"`
	Time        time.Time       `json:"time"`
	PayloadType string          `json:"payload_type,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// runsSubdir is where a session's recorded batches live, relative to the
// session directory. Keeping them inside the session makes one conversation's
// data self-contained: deleting the session deletes its recordings, and the
// session id is part of the path instead of being inferred from a directory
// name.
const runsSubdir = "runs"

// DefaultBaseDir returns the directory that holds every session, including its
// recorded batches at <session>/runs/.
func DefaultBaseDir() string {
	return fs.NekocodeDataDir("sessions")
}

// NewEventRecorder writes registered runs under <sessionsRoot>/<session>/runs/.
// Direct Record callers without a known session use a provisional timestamp;
// runtime registers each run explicitly before allowing its first event.
func NewEventRecorder(sessionsRoot string) (*EventRecorder, error) {
	if strings.TrimSpace(sessionsRoot) == "" {
		return nil, fmt.Errorf("runtime: empty event recorder base dir")
	}
	if err := os.MkdirAll(sessionsRoot, 0o700); err != nil {
		return nil, err
	}
	pruneEmptyBatches(sessionsRoot)

	sessionID := time.Now().UTC().Format("20060102T150405.000000000Z")
	sessionID = strings.NewReplacer(":", "", ".", "-").Replace(sessionID)
	return &EventRecorder{
		baseDir:       sessionsRoot,
		sessionID:     sessionID,
		provisionalID: sessionID,
		runs:          make(map[core.RunID]*runRecording),
	}, nil
}

// BindSession sets the default for direct Record callers and binds any
// provisional runs. Runtime uses BeginRun/BindRunSession instead, so changing
// the default never redirects events belonging to an already registered run.
func (r *EventRecorder) BindSession(sessionID string) error {
	if r == nil {
		return nil
	}
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, run := range r.runs {
		if err := r.bindRunLocked(id, run, sessionID); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(r.baseDir, sessionID, runsSubdir), 0o700); err != nil {
		return err
	}
	r.sessionID = sessionID
	return nil
}

// BeginRun fixes this run's destination before any of its events can arrive.
// An empty session is provisional until BindRunSession supplies its identity.
func (r *EventRecorder) BeginRun(id core.RunID, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beginRunLocked(id, sessionID)
}

func (r *EventRecorder) beginRunLocked(id core.RunID, sessionID string) error {
	if id == "" {
		return fmt.Errorf("runtime: empty recorded run id")
	}
	if _, ok := r.runs[id]; ok {
		return nil
	}
	provisional := sessionID == "" || sessionID == r.provisionalID
	if sessionID == "" {
		sessionID = r.provisionalID + "_" + safePathPart(string(id))
	}
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	dir := filepath.Join(r.baseDir, sessionID, runsSubdir, safePathPart(string(id)))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, acquired, err := lockRun(dir)
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("runtime: recorded run is active: %s", id)
	}
	if _, err := os.Lstat(filepath.Join(dir, "events.jsonl")); !os.IsNotExist(err) {
		_ = lock.Close()
		if err != nil {
			return err
		}
		return fmt.Errorf("runtime: recorded run identity collision: %s", id)
	}
	r.runs[id] = &runRecording{dir: dir, provisional: provisional, lock: lock}
	return nil
}

// BindRunSession only moves a provisional run; a known session is immutable.
func (r *EventRecorder) BindRunSession(id core.RunID, sessionID string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bindRunLocked(id, r.runs[id], sessionID)
}

func (r *EventRecorder) bindRunLocked(id core.RunID, run *runRecording, sessionID string) error {
	if run == nil || !run.provisional {
		return nil
	}
	target := filepath.Join(r.baseDir, sessionID, runsSubdir, safePathPart(string(id)))
	if run.dir == target {
		run.provisional = false
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		return fmt.Errorf("runtime: recorded run identity collision: %s", id)
	}
	if err := os.Rename(run.dir, target); err != nil {
		return err
	}
	oldRuns := filepath.Dir(run.dir)
	run.dir, run.provisional = target, false
	_ = os.Remove(oldRuns)
	_ = os.Remove(filepath.Dir(oldRuns))
	return nil
}

// EndRun releases retention protection after every terminal event is recorded.
func (r *EventRecorder) EndRun(id core.RunID) {
	r.mu.Lock()
	if run := r.runs[id]; run != nil {
		_ = run.lock.Close()
		delete(r.runs, id)
	}
	prune := !r.lastPrune.IsZero() && time.Since(r.lastPrune) >= time.Minute
	if prune {
		r.lastPrune = time.Now()
	}
	r.mu.Unlock()
	if prune {
		if err := PruneRecordedRuns(r.baseDir, RetentionAge, RetentionBytes); err != nil {
			logger.Log("runtime: prune recorded runs: %v", err)
		}
	}
}

// StartRetention enables periodic cleanup after startup restore has finished.
// Until then, EndRun must not delete files still queued for restore.
func (r *EventRecorder) StartRetention() {
	r.mu.Lock()
	r.lastPrune = time.Now()
	r.mu.Unlock()
}

// validateSessionID rejects anything that would escape the base directory.
func validateSessionID(id string) error {
	if strings.TrimSpace(id) == "" || id == "." || id == ".." ||
		id != filepath.Base(id) || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid session id: %q", id)
	}
	return nil
}

// runNumber parses the numeric part of a run_<N> directory name.
func runNumber(name string) (uint64, bool) {
	if !strings.HasPrefix(name, "run_") {
		return 0, false
	}
	number, _, _ := strings.Cut(strings.TrimPrefix(name, "run_"), "_")
	n, err := strconv.ParseUint(number, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (r *EventRecorder) SessionID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

func (r *EventRecorder) RunDir(runID core.RunID) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if run := r.runs[runID]; run != nil {
		return run.dir
	}
	return filepath.Join(r.baseDir, r.sessionID, runsSubdir, safePathPart(string(runID)))
}

func (r *EventRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, run := range r.runs {
		_ = run.lock.Close()
		delete(r.runs, id)
	}
	return r.writeErr
}

func (r *EventRecorder) Record(ev core.Event) {
	if err := r.RecordError(ev); err != nil {
		logger.Log("runtime: event recorder: %v", err)
	}
}

func (r *EventRecorder) RecordError(ev core.Event) (err error) {
	if r == nil || ev.RunID == "" || ev.Type == core.EventContextUpdated {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if err != nil && r.writeErr == nil {
			r.writeErr = err
		}
	}()

	if err := r.beginRunLocked(ev.RunID, r.sessionID); err != nil {
		return err
	}
	runDir := r.runs[ev.RunID].dir
	if err := appendJSONLine(filepath.Join(runDir, "events.jsonl"), recordedEventFrom(ev)); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func recordedEventFrom(ev core.Event) recordedEvent {
	payload, payloadType := marshalPayload(ev.Payload)
	return recordedEvent{
		Version:     ev.Version,
		ID:          ev.ID,
		Sequence:    ev.Sequence,
		RunID:       ev.RunID,
		Type:        ev.Type,
		Source:      ev.Source,
		Time:        ev.Time,
		PayloadType: payloadType,
		Payload:     payload,
	}
}

func LoadRecordedEvents(baseDir string) ([]core.Event, error) {
	runs, err := ScanRecordedRuns(baseDir, 0)
	if err != nil {
		return nil, err
	}
	return LoadRecordedEventFiles(runs), nil
}

// LoadRecentRecordedEvents restores only the newest run files. Runtime's
// in-memory run store is bounded, so decoding older files would consume startup
// time only for those records to be discarded immediately.
func LoadRecentRecordedEvents(baseDir string, runLimit int) ([]core.Event, error) {
	if runLimit <= 0 {
		return nil, nil
	}
	runs, err := ScanRecordedRuns(baseDir, runLimit)
	if err != nil {
		return nil, err
	}
	return LoadRecordedEventFiles(runs), nil
}

// RecordedRun locates one recorded run's event file.
type RecordedRun struct {
	// Path is the events.jsonl file of the run.
	Path string
	// Sequence is the run_<N> number parsed from the directory name.
	Sequence uint64
}

// ScanRecordedRuns lists the newest recorded runs without reading any file
// content. Startup needs run ordering and sequence numbers before the heavy
// JSON decoding happens — names alone answer both.
//
// sessionsRoot holds one directory per session, each with its recordings at
// <session>/runs/<run>/events.jsonl.
func ScanRecordedRuns(sessionsRoot string, runLimit int) ([]RecordedRun, error) {
	if strings.TrimSpace(sessionsRoot) == "" {
		return nil, fmt.Errorf("runtime: empty event recorder base dir")
	}
	matches, err := filepath.Glob(filepath.Join(sessionsRoot, "*", runsSubdir, "*", "events.jsonl"))
	if err != nil {
		return nil, err
	}
	sortRecordedRunFiles(matches)
	if runLimit > 0 && len(matches) > runLimit {
		matches = matches[len(matches)-runLimit:]
	}
	runs := make([]RecordedRun, len(matches))
	for i, path := range matches {
		_, seq := recordedRunOrder(path)
		runs[i] = RecordedRun{Path: path, Sequence: seq}
	}
	return runs, nil
}

// LoadRecordedEventFiles parses the given recorded runs in parallel and
// returns their events ordered by time.
func LoadRecordedEventFiles(runs []RecordedRun) []core.Event {
	if len(runs) == 0 {
		return nil
	}
	loaded := make([][]core.Event, len(runs))
	workers := min(runtime.GOMAXPROCS(0), len(runs))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				events, err := loadRecordedEventFile(runs[i].Path)
				if err != nil {
					logger.Log("runtime: skip recorded events %s: %v", runs[i].Path, err)
					continue
				}
				loaded[i] = events
			}
		}()
	}
	for i := range runs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	var out []core.Event
	for _, events := range loaded {
		out = append(out, events...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Time.Before(out[j].Time)
	})
	return out
}

func sortRecordedRunFiles(paths []string) {
	modified := make(map[string]time.Time, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			modified[path] = info.ModTime()
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		if !modified[paths[i]].Equal(modified[paths[j]]) {
			return modified[paths[i]].Before(modified[paths[j]])
		}
		leftBatch, leftRun := recordedRunOrder(paths[i])
		rightBatch, rightRun := recordedRunOrder(paths[j])
		if leftBatch != rightBatch {
			return leftBatch < rightBatch
		}
		if leftRun != rightRun {
			return leftRun < rightRun
		}
		return paths[i] < paths[j]
	})
}

func recordedRunOrder(path string) (string, uint64) {
	runDir := filepath.Base(filepath.Dir(path))
	// <session>/runs/<run>/events.jsonl: skip the runs/ level to reach the session.
	session := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	run, _ := runNumber(runDir)
	return session, run
}

// pruneEmptyBatches removes empty recordings directories left behind by
// interrupted writes. It only ever looks at <session>/runs/, never the session
// itself: a session directory is owned by the session store, and an empty one
// may simply be a session that has not written its snapshot yet.
func pruneEmptyBatches(sessionsRoot string) {
	sessions, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return
	}
	for _, session := range sessions {
		if !session.IsDir() {
			continue
		}
		dir := filepath.Join(sessionsRoot, session.Name(), runsSubdir)
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
}

func loadRecordedEventFile(path string) ([]core.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var events []core.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec recordedEvent
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			logger.Log("runtime: recorded event %s:%d unmarshal: %v", path, lineNo, err)
			continue
		}
		ev, err := rec.Event()
		if err != nil {
			logger.Log("runtime: recorded event %s:%d decode: %v", path, lineNo, err)
			continue
		}
		events = append(events, ev)
	}
	return events, scanner.Err()
}

func (r recordedEvent) Event() (core.Event, error) {
	if (r.Version == "" || r.Version == "1.0") && r.Type == core.EventType("session_resumed") {
		r.Type = core.EventSessionChanged
	}
	payload, err := decodeRecordedPayload(r.Version, r.Type, r.Payload)
	if err != nil {
		return core.Event{}, err
	}
	return core.Event{
		Version:  core.ProtocolVersion,
		ID:       r.ID,
		Sequence: r.Sequence,
		RunID:    r.RunID,
		Type:     r.Type,
		Source:   r.Source,
		Time:     r.Time,
		Payload:  payload,
	}, nil
}

func decodeRecordedPayload(version string, typ core.EventType, data json.RawMessage) (any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if (version == "" || version == "1.0") && (typ == core.EventSubAgentStarted || typ == core.EventSubAgentEnded) {
		var legacy core.ToolPayload
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, err
		}
		color, _ := strconv.Atoi(legacy.Output)
		return core.SubAgentPayload{ID: legacy.Args, Type: legacy.ToolName, Color: color}, nil
	}
	return core.DecodeEventPayload(typ, data)
}

func marshalPayload(payload any) (json.RawMessage, string) {
	if payload == nil {
		return nil, ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		data, _ = json.Marshal(map[string]string{"error": err.Error()})
		return data, "marshal_error"
	}
	return data, payloadTypeName(payload)
}

func payloadTypeName(payload any) string {
	return strings.Replace(fmt.Sprintf("%T", payload), "core.", "runtime.", 1)
}

func appendJSONLine(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func safePathPart(s string) string {
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Retention limits apply to individual runs. Active runs are protected by an
// OS file lock, including runs owned by another process.
const (
	RetentionAge   = 30 * 24 * time.Hour
	RetentionBytes = 500 << 20
)

// PruneRecordedRuns removes expired runs, then oldest inactive runs until the
// byte budget is met. Session snapshots and active runs are never removed.
func PruneRecordedRuns(sessionsRoot string, maxAge time.Duration, maxBytes int64) error {
	if strings.TrimSpace(sessionsRoot) == "" {
		return fmt.Errorf("runtime: empty event recorder base dir")
	}
	files, err := filepath.Glob(filepath.Join(sessionsRoot, "*", runsSubdir, "*", "events.jsonl"))
	if err != nil {
		return err
	}
	type candidate struct {
		path string
		mod  time.Time
		size int64
	}
	var runs []candidate
	var total int64
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		runs = append(runs, candidate{path, info.ModTime(), info.Size()})
		total += info.Size()
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].mod.Equal(runs[j].mod) {
			return runs[i].path < runs[j].path
		}
		return runs[i].mod.Before(runs[j].mod)
	})
	for _, run := range runs {
		expired := maxAge > 0 && time.Since(run.mod) > maxAge
		if !expired && (maxBytes <= 0 || total <= maxBytes) {
			continue
		}
		dir := filepath.Dir(run.path)
		lock, acquired, err := lockRun(dir)
		if os.IsNotExist(err) {
			total -= run.size
			continue
		}
		if err != nil {
			return err
		}
		if !acquired {
			continue
		}
		// A writer may have finished between the scan and lock acquisition.
		info, err := os.Stat(run.path)
		if err != nil {
			_ = lock.Close()
			if os.IsNotExist(err) {
				total -= run.size
				continue
			}
			return err
		}
		total += info.Size() - run.size
		expired = maxAge > 0 && time.Since(info.ModTime()) > maxAge
		if !expired && (maxBytes <= 0 || total <= maxBytes) {
			_ = lock.Close()
			continue
		}
		// IDs are never reused. Once the lock is free, a completed run has no
		// future writer; closing also permits directory removal on Windows.
		_ = lock.Close()
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		total -= info.Size()
		logger.Log("runtime: pruned recorded run %s", dir)
		_ = os.Remove(filepath.Dir(dir)) // only succeeds if runs/ is empty
	}
	return nil
}
