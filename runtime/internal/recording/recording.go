package recording

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
	"nekocode/util/fs"
)

type EventRecorder struct {
	mu        sync.Mutex
	sessionID string
	runDir    string
	writeErr  error
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

func DefaultBaseDir() string {
	return fs.NekocodeDataDir("runs")
}

func NewEventRecorder(baseDir string) (*EventRecorder, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("runtime: empty event recorder base dir")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, err
	}
	pruneEmptyBatches(baseDir)

	sessionID := time.Now().UTC().Format("20060102T150405.000000000Z")
	sessionID = strings.NewReplacer(":", "", ".", "-").Replace(sessionID)
	runDir := filepath.Join(baseDir, sessionID)
	return &EventRecorder{
		sessionID: sessionID,
		runDir:    runDir,
	}, nil
}

func (r *EventRecorder) SessionID() string {
	if r == nil {
		return ""
	}
	return r.sessionID
}

func (r *EventRecorder) RunDir(runID core.RunID) string {
	if r == nil {
		return ""
	}
	return filepath.Join(r.runDir, safePathPart(string(runID)))
}

func (r *EventRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writeErr
}

func (r *EventRecorder) Record(ev core.Event) {
	if err := r.RecordError(ev); err != nil {
		log.Printf("runtime: event recorder: %v", err)
	}
}

func (r *EventRecorder) RecordError(ev core.Event) error {
	if r == nil || ev.RunID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	runDir := r.RunDir(ev.RunID)
	if err := appendJSONLine(filepath.Join(runDir, "events.jsonl"), recordedEventFrom(ev)); err != nil {
		err = fmt.Errorf("append event: %w", err)
		if r.writeErr == nil {
			r.writeErr = err
		}
		return err
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
	return loadRecordedEvents(baseDir, 0)
}

// LoadRecentRecordedEvents restores only the newest run files. Runtime's
// in-memory run store is bounded, so decoding older files would consume startup
// time only for those records to be discarded immediately.
func LoadRecentRecordedEvents(baseDir string, runLimit int) ([]core.Event, error) {
	if runLimit <= 0 {
		return nil, nil
	}
	return loadRecordedEvents(baseDir, runLimit)
}

func loadRecordedEvents(baseDir string, runLimit int) ([]core.Event, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("runtime: empty event recorder base dir")
	}
	matches, err := filepath.Glob(filepath.Join(baseDir, "*", "*", "events.jsonl"))
	if err != nil {
		return nil, err
	}
	sortRecordedRunFiles(matches)
	if runLimit > 0 && len(matches) > runLimit {
		matches = matches[len(matches)-runLimit:]
	}

	loaded := make([][]core.Event, len(matches))
	workers := min(runtime.GOMAXPROCS(0), len(matches))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				events, err := loadRecordedEventFile(matches[i])
				if err != nil {
					log.Printf("runtime: skip recorded events %s: %v", matches[i], err)
					continue
				}
				loaded[i] = events
			}
		}()
	}
	for i := range matches {
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
	return out, nil
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
	batch := filepath.Base(filepath.Dir(filepath.Dir(path)))
	run, _ := strconv.ParseUint(strings.TrimPrefix(runDir, "run_"), 10, 64)
	return batch, run
}

func pruneEmptyBatches(baseDir string) {
	batches, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	for _, batch := range batches {
		if !batch.IsDir() {
			continue
		}
		path := filepath.Join(baseDir, batch.Name())
		entries, err := os.ReadDir(path)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(path)
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
			log.Printf("runtime: recorded event %s:%d unmarshal: %v", path, lineNo, err)
			continue
		}
		ev, err := rec.Event()
		if err != nil {
			log.Printf("runtime: recorded event %s:%d decode: %v", path, lineNo, err)
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
	switch typ {
	case core.EventCompaction:
		var p protocol.CompactionEvent
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		if !p.Status.Valid() || !p.Trigger.Valid() {
			return nil, fmt.Errorf("invalid compaction event status %q or trigger %q", p.Status, p.Trigger)
		}
		return p, nil
	case core.EventInputAccepted, core.EventSystemMessage:
		var p core.MessagePayload
		return p, json.Unmarshal(data, &p)
	case core.EventAssistantDelta, core.EventReasoningDelta:
		var p core.DeltaPayload
		return p, json.Unmarshal(data, &p)
	case core.EventPhaseChanged:
		var p core.PhasePayload
		return p, json.Unmarshal(data, &p)
	case core.EventToolStarted, core.EventToolBlocked, core.EventToolPreview, core.EventToolCompleted:
		var p core.ToolPayload
		return p, json.Unmarshal(data, &p)
	case core.EventSubAgentStarted, core.EventSubAgentEnded:
		if version == "" || version == "1.0" {
			var legacy core.ToolPayload
			if err := json.Unmarshal(data, &legacy); err != nil {
				return nil, err
			}
			color, _ := strconv.Atoi(legacy.Output)
			return core.SubAgentPayload{ID: legacy.Args, Type: legacy.ToolName, Color: color}, nil
		}
		var p core.SubAgentPayload
		return p, json.Unmarshal(data, &p)
	case core.EventSessionChanged:
		var p core.SessionPayload
		return p, json.Unmarshal(data, &p)
	case core.EventTodosUpdated:
		var p []protocol.TodoItem
		return p, json.Unmarshal(data, &p)
	case core.EventApprovalRequested, core.EventApprovalResolved:
		var p core.ApprovalView
		return p, json.Unmarshal(data, &p)
	case core.EventQuestionRequested, core.EventQuestionResolved:
		var p core.QuestionView
		return p, json.Unmarshal(data, &p)
	case core.EventRunDone, core.EventRunFailed, core.EventRunCancelled:
		var p core.RunResult
		return p, json.Unmarshal(data, &p)
	case core.EventConnectorStatus:
		var p core.ConnectorStatusPayload
		return p, json.Unmarshal(data, &p)
	case core.EventMetricsUpdated:
		var p protocol.Metrics
		return p, json.Unmarshal(data, &p)
	default:
		var p map[string]any
		return p, json.Unmarshal(data, &p)
	}
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
