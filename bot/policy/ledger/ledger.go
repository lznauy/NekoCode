// Package ledger records concrete tool outcomes used by deterministic policy.
package ledger

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"

	"nekocode/protocol"
)

type pathSet map[string]struct{}

func extractPaths(args map[string]any) []string {
	if p, _ := args["path"].(string); p != "" {
		return []string{filepath.Clean(p)}
	}
	return nil
}

func newPathSetFrom(paths []string) pathSet {
	s := make(pathSet, len(paths))
	s.addAll(paths)
	return s
}

func (s pathSet) addAll(paths []string) {
	for _, path := range paths {
		if path != "" {
			s[filepath.Clean(path)] = struct{}{}
		}
	}
}

func (s pathSet) sorted() []string {
	return slices.Sorted(maps.Keys(s))
}

// ToolEvent is one observed tool outcome. It intentionally carries no inferred
// task semantics such as "verification" or "exploration".
type ToolEvent struct {
	Name      string
	Args      map[string]any
	Output    string
	Error     string
	Blocked   bool
	BlockText string
}

// MarshalJSON projects execution arguments into durable audit input. This also
// protects snapshots supplied directly by programmatic callers or restored
// outside RecordTool, without changing the live tool invocation.
func (ev ToolEvent) MarshalJSON() ([]byte, error) {
	type wireEvent ToolEvent
	if ev.Args != nil {
		ev.Args = protocol.ToolInputArgs(ev.Args)
	}
	return json.Marshal(wireEvent(ev))
}

// Ledger owns current-run read evidence and run-level audit evidence.
type Ledger struct {
	mu sync.RWMutex

	readFiles      pathSet
	modifiedFiles  pathSet
	blockedTools   []ToolEvent
	toolErrors     []ToolEvent
	toolEventCount int
}

// New creates an empty ledger.
func New() *Ledger {
	return &Ledger{
		readFiles:     make(pathSet),
		modifiedFiles: make(pathSet),
	}
}

// ResetRun clears all run evidence, including prior reads. A read from an
// earlier run is not proof that the file still has the same contents.
func (l *Ledger) ResetRun() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.readFiles = make(pathSet)
	l.modifiedFiles = make(pathSet)
	l.blockedTools = nil
	l.toolErrors = nil
	l.toolEventCount = 0
}

// RecordTool records only outcomes that can be established directly from the
// tool identity and result. Shell command text is never interpreted as proof
// that a file was read, modified, or verified.
func (l *Ledger) RecordTool(ev ToolEvent) {
	l.recordTool(ev, true)
}

// RecordAuditTool records run-level audit facts without creating read
// authorization. It is used when aggregating outcomes from another actor.
func (l *Ledger) RecordAuditTool(ev ToolEvent) {
	l.recordTool(ev, false)
}

func (l *Ledger) recordTool(ev ToolEvent, authorizeRead bool) {
	// Keep executable callbacks and display metadata out of durable audit state.
	if ev.Args != nil {
		ev.Args = protocol.ToolInputArgs(ev.Args)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.toolEventCount++
	if ev.Blocked {
		l.blockedTools = append(l.blockedTools, ev)
		return
	}
	if ev.Error != "" {
		l.toolErrors = append(l.toolErrors, ev)
		return
	}

	switch ev.Name {
	case "read":
		if authorizeRead {
			l.readFiles.addAll(extractPaths(ev.Args))
		}
	case "write", "edit":
		modified := extractPaths(ev.Args)
		l.modifiedFiles.addAll(modified)
		if authorizeRead && ev.Name == "write" {
			// A successful full write establishes the exact current content.
			l.readFiles.addAll(modified)
		}
	}
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{
		ReadFiles:      l.readFiles.sorted(),
		ModifiedFiles:  l.modifiedFiles.sorted(),
		ToolEventCount: l.toolEventCount,
	}
	s.BlockedTools = append(s.BlockedTools, l.blockedTools...)
	s.ToolErrors = append(s.ToolErrors, l.toolErrors...)
	return s
}

func (l *Ledger) Restore(s Snapshot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Persisted ReadFiles may come from an older version that inferred reads
	// from shell text. Keep historical snapshots loadable, but never restore
	// them as authorization evidence for a new run.
	l.readFiles = make(pathSet)
	l.modifiedFiles = newPathSetFrom(s.ModifiedFiles)
	l.blockedTools = append([]ToolEvent(nil), s.BlockedTools...)
	l.toolErrors = append([]ToolEvent(nil), s.ToolErrors...)
	l.toolEventCount = s.ToolEventCount
}

// Snapshot is the persisted ledger state.
type Snapshot struct {
	ReadFiles      []string
	ModifiedFiles  []string
	BlockedTools   []ToolEvent
	ToolErrors     []ToolEvent
	ToolEventCount int
}

// WasRead reports whether, in the current run, the dedicated read tool
// successfully read path or a successful full write established its contents.
func (l *Ledger) WasRead(path string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if path == "" {
		return false
	}
	_, ok := l.readFiles[filepath.Clean(path)]
	return ok
}

// HasModifications reports whether the snapshot contains any modified files.
// Deprecated: inspect ModifiedFiles directly when consuming a snapshot.
func (s Snapshot) HasModifications() bool {
	return len(s.ModifiedFiles) > 0
}

func (s Snapshot) Summary() string {
	return fmt.Sprintf("%d modified, %d tool errors, %d blocked tools",
		len(s.ModifiedFiles), len(s.ToolErrors), len(s.BlockedTools))
}
