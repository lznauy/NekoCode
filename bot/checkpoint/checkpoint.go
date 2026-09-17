// Package checkpoint stores per-message file snapshots and restores them on rewind.
package checkpoint

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"nekocode/util/fs"
)

const (
	manifestName       = "manifest.json"
	rewindJournalName  = "rewind.json"
	rewindRollbackDir  = "rollback"
	macKeyName         = ".auth-key"
	macPendingKeyName  = ".auth-key.pending"
	MaxTurnsPerSession = 100
	maxMessageRunes    = 120
)

type ChangeKind string

const (
	ChangeCreated  ChangeKind = "created"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
)

type entry struct {
	Path     string     `json:"path"`
	Before   string     `json:"before"`           // absent | file
	Change   ChangeKind `json:"change,omitempty"` // created | modified | deleted
	Snapshot string     `json:"snapshot,omitempty"`
	Digest   string     `json:"snapshot_sha256,omitempty"`
	Mode     uint32     `json:"mode,omitempty"`
}

type manifest struct {
	Version     int       `json:"version,omitempty"`
	Session     string    `json:"session,omitempty"`
	Turn        string    `json:"turn"`
	UserMessage string    `json:"user_message,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Entries     []entry   `json:"entries"`
	MAC         string    `json:"mac,omitempty"`
}

type Result struct {
	RewindID    string
	Turn        string
	UserMessage string
	Changes     []RollbackChange
}

type PartialRewindError struct {
	Cause   error
	Changes []RollbackChange
}

func (e *PartialRewindError) Error() string {
	return "checkpoint: rewind rollback was incomplete: " + e.Cause.Error()
}
func (e *PartialRewindError) Unwrap() error { return e.Cause }

type RollbackChange struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

const (
	RollbackRemovedCreatedFile  = "removed_created_file"
	RollbackRestoredFile        = "restored_previous_file"
	RollbackRestoredDeletedFile = "restored_deleted_file"
)

type FileChange struct {
	Path string
	Kind ChangeKind
}

type TurnInfo struct {
	Turn        string
	UserMessage string
	CreatedAt   time.Time
	Changes     []FileChange
}

type Manager struct {
	mu sync.Mutex

	root          string
	turns         map[string][]string
	next          map[string]int
	activeSession string
	active        manifest
	inFlight      map[string]int
	macKey        []byte
	pending       map[string]map[string]pendingRewind
}

func defaultRoot() string { return fs.NekocodeDataDir("checkpoints") }

func New(root string) *Manager {
	if root == "" {
		root = defaultRoot()
	}
	return &Manager{root: root, turns: make(map[string][]string), next: make(map[string]int), pending: make(map[string]map[string]pendingRewind)}
}

// Activate installs the persisted turn index for a session. It does not begin
// a new turn; Begin is called only when the agent handles user input.
func (m *Manager) Activate(session string, turns []string, next int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeSession = ""
	m.active = manifest{}
	m.inFlight = nil
	if !validID(session) {
		return
	}
	cleaned := cleanTurns(turns)
	if highest := highestTurn(cleaned); next < highest {
		next = highest
	}
	if err := m.ensureRecovered(session); err != nil {
		m.turns[session] = cleaned
		m.next[session] = next
		return
	}
	m.turns[session] = m.existingTurns(session, cleaned)
	m.next[session] = next
}

// Begin starts a checkpoint without message metadata. It is retained for
// callers that do not have the originating user message.
func (m *Manager) Begin(session string) (string, error) {
	return m.BeginMessage(session, "")
}

// BeginMessage starts a checkpoint anchored to a display-safe preview of the
// originating user message.
func (m *Manager) BeginMessage(session, userMessage string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.beginMessage(session, userMessage)
}

func (m *Manager) beginMessage(session, userMessage string) (string, error) {
	if !validID(session) {
		return "", fmt.Errorf("checkpoint: invalid session %q", session)
	}
	if err := m.ensureRecovered(session); err != nil {
		return "", err
	}
	if m.activeSession != "" || m.active.Turn != "" {
		return "", fmt.Errorf("checkpoint: turn %s is still active for session %s", m.active.Turn, m.activeSession)
	}
	m.next[session]++
	turn := strconv.Itoa(m.next[session])
	dir := m.turnDir(session, turn)
	if err := secureDir(m.root); err != nil {
		m.next[session]--
		return "", fmt.Errorf("checkpoint: secure root: %w", err)
	}
	if err := secureDir(filepath.Join(m.root, session)); err != nil {
		m.next[session]--
		return "", fmt.Errorf("checkpoint: secure session: %w", err)
	}
	if err := secureDir(dir); err != nil {
		m.next[session]--
		return "", fmt.Errorf("checkpoint: create turn: %w", err)
	}
	next := manifest{Session: session, Turn: turn, UserMessage: messagePreview(userMessage), CreatedAt: time.Now()}
	if err := m.writeManifest(dir, next); err != nil {
		m.next[session]--
		return "", fmt.Errorf("checkpoint: write turn: %w", err)
	}
	m.activeSession = session
	m.active = next
	m.inFlight = make(map[string]int)
	m.turns[session] = append(m.turns[session], turn)
	return turn, nil
}

// Finish closes the active turn and retains the latest one hundred complete
// user-message anchors. Empty turns remain useful rewind points because later
// file changes can still be restored to the state before that message.
func (m *Manager) Finish(session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finish(session)
}

func (m *Manager) finish(session string) error {
	if m.activeSession == "" || m.active.Turn == "" {
		return nil
	}
	if m.activeSession != session {
		return fmt.Errorf("checkpoint: active session is %q, not %q", m.activeSession, session)
	}
	if len(m.inFlight) > 0 {
		return fmt.Errorf("checkpoint: cannot finish while file tools are running")
	}
	// The anchor is complete before retention cleanup begins. Clear it first so
	// a failed best-effort prune cannot permanently block the next message.
	m.activeSession = ""
	m.active = manifest{}
	m.inFlight = nil
	turns := m.turns[session]
	for len(turns) > MaxTurnsPerSession {
		oldest := turns[0]
		if err := os.RemoveAll(m.turnDir(session, oldest)); err != nil {
			m.turns[session] = append([]string(nil), turns...)
			log.Printf("checkpoint: defer pruning turn %s: %v", oldest, err)
			return nil
		}
		turns = turns[1:]
		m.turns[session] = append([]string(nil), turns...)
	}
	m.turns[session] = append([]string(nil), turns...)
	return nil
}

// RotateMessage closes the current message anchor and opens the next one. It
// is used when steering adds another user message inside a running agent call.
func (m *Manager) RotateMessage(userMessage string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeSession == "" || m.active.Turn == "" {
		return fmt.Errorf("checkpoint: cannot rotate without an active message")
	}
	session := m.activeSession
	previous := m.active
	finishErr := m.finish(session)
	if m.activeSession != "" {
		return finishErr
	}
	if _, err := m.beginMessage(session, userMessage); err != nil {
		// The previous manifest remains valid and indexed after Finish. Restore
		// it as the active safety net so later file tools cannot run untracked.
		m.activeSession = session
		m.active = previous
		m.inFlight = make(map[string]int)
		return errors.Join(finishErr, err)
	}
	return finishErr
}

// Capture records path's state once for the active turn. A capture failure is
// returned to the executor so a write cannot proceed without a rewind point.
func (m *Manager) Capture(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeSession == "" || m.active.Turn == "" {
		return nil
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("checkpoint: path must be absolute: %s", path)
	}
	for _, entry := range m.active.Entries {
		if entry.Path == path {
			m.inFlight[path]++
			return nil
		}
	}

	entry := entry{Path: path, Before: "absent"}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		entry.Change = ChangeCreated
	case err != nil:
		return fmt.Errorf("checkpoint: inspect %s: %w", path, err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("checkpoint: unsupported non-file target %s", path)
	default:
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("checkpoint: read %s: %w", path, readErr)
		}
		entry.Before = "file"
		entry.Change = ChangeModified
		entry.Mode = uint32(info.Mode().Perm())
		entry.Snapshot = snapshotName(path)
		entry.Digest = contentDigest(data)
		if err := writePrivateFile(filepath.Join(m.activeDir(), entry.Snapshot), data); err != nil {
			return fmt.Errorf("checkpoint: snapshot %s: %w", path, err)
		}
		if err := syncPath(filepath.Join(m.activeDir(), entry.Snapshot)); err != nil {
			return fmt.Errorf("checkpoint: sync snapshot %s: %w", path, err)
		}
	}

	m.active.Entries = append(m.active.Entries, entry)
	if err := m.writeManifest(m.activeDir(), m.active); err != nil {
		m.active.Entries = m.active.Entries[:len(m.active.Entries)-1]
		if entry.Snapshot != "" {
			_ = os.Remove(filepath.Join(m.activeDir(), entry.Snapshot))
		}
		return fmt.Errorf("checkpoint: update manifest: %w", err)
	}
	m.inFlight[path]++
	return nil
}

// Finalize records the observed change kind and removes no-op captures.
func (m *Manager) Finalize(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeSession == "" || m.active.Turn == "" {
		return nil
	}
	path = filepath.Clean(path)
	if count := m.inFlight[path]; count > 1 {
		m.inFlight[path] = count - 1
		return nil
	} else {
		delete(m.inFlight, path)
	}
	for i := range m.active.Entries {
		entry := &m.active.Entries[i]
		if entry.Path != path {
			continue
		}
		changed, kind, err := m.changed(*entry)
		if err != nil {
			return err
		}
		if !changed {
			if entry.Snapshot != "" {
				_ = os.Remove(filepath.Join(m.activeDir(), entry.Snapshot))
			}
			m.active.Entries = append(m.active.Entries[:i], m.active.Entries[i+1:]...)
		} else {
			entry.Change = kind
		}
		return m.writeManifest(m.activeDir(), m.active)
	}
	return nil
}

func (m *Manager) Index(session string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.turns[session]...)
}

func (m *Manager) Next(session string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.next[session]
}

// History returns user-message checkpoints newest first.
func (m *Manager) History(session string) ([]TurnInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureRecovered(session); err != nil {
		return nil, err
	}
	turns := m.existingTurns(session, m.turns[session])
	m.turns[session] = turns
	history := make([]TurnInfo, 0, len(turns))
	for i := len(turns) - 1; i >= 0; i-- {
		mf, err := m.readManifest(m.turnDir(session, turns[i]))
		if err != nil {
			return nil, err
		}
		if err := validateManifest(mf, session, turns[i]); err != nil {
			return nil, err
		}
		info := TurnInfo{
			Turn: mf.Turn, UserMessage: messagePreview(mf.UserMessage), CreatedAt: mf.CreatedAt,
			Changes: make([]FileChange, 0, len(mf.Entries)),
		}
		for _, item := range mf.Entries {
			info.Changes = append(info.Changes, FileChange{Path: item.Path, Kind: item.Change})
		}
		history = append(history, info)
	}
	return history, nil
}

// Rewind restores the requested turn anchor and every newer turn. An empty
// target selects the latest turn. Rewound anchors are removed after success.
func (m *Manager) Rewind(session, target string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureRecovered(session); err != nil {
		return Result{}, err
	}
	turns := m.existingTurns(session, m.turns[session])
	m.turns[session] = turns
	if len(turns) == 0 {
		return Result{}, fmt.Errorf("checkpoint: no turns available")
	}
	if len(m.inFlight) > 0 {
		return Result{}, fmt.Errorf("checkpoint: cannot rewind while file tools are running")
	}
	if target == "" {
		target = turns[len(turns)-1]
	}
	idx := indexOf(turns, target)
	if idx < 0 {
		return Result{}, fmt.Errorf("checkpoint: turn %q not found (available: %s)", target, strings.Join(turns, ", "))
	}

	result := Result{Turn: target}
	plans := make(map[string]plannedRestore)
	for i := len(turns) - 1; i >= idx; i-- {
		turn := turns[i]
		mf, err := m.readManifest(m.turnDir(session, turn))
		if err != nil {
			return result, err
		}
		if err := validateManifest(mf, session, turn); err != nil {
			return result, err
		}
		if turn == target {
			result.UserMessage = messagePreview(mf.UserMessage)
		}
		for j := len(mf.Entries) - 1; j >= 0; j-- {
			item := mf.Entries[j]
			plan := plannedRestore{mode: os.FileMode(item.Mode), turn: turn, snapshot: item.Snapshot, digest: item.Digest}
			switch item.Before {
			case "absent":
				plan.exists = false
			case "file":
				info, statErr := os.Lstat(filepath.Join(m.turnDir(session, turn), item.Snapshot))
				if statErr != nil {
					return result, fmt.Errorf("checkpoint: inspect snapshot for %s: %w", item.Path, statErr)
				}
				if !info.Mode().IsRegular() {
					return result, fmt.Errorf("checkpoint: snapshot for %s is not a regular file", item.Path)
				}
				plan.exists = true
			default:
				return result, fmt.Errorf("checkpoint: invalid state %q for %s", item.Before, item.Path)
			}
			action := RollbackRestoredFile
			switch {
			case item.Before == "absent":
				action = RollbackRemovedCreatedFile
			case item.Change == ChangeDeleted:
				action = RollbackRestoredDeletedFile
			}
			plan.action = action
			plans[item.Path] = plan
		}
	}
	paths := make([]string, 0, len(plans))
	for path := range plans {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result.Changes = rollbackChangesForPaths(paths, plans)
	// Move consumed anchors aside before touching the workspace. This makes the
	// rewind durable even if saving the updated session index later fails: old
	// persisted indexes will reconcile the now-missing turn directories. The
	// moves are reversible until the workspace commit succeeds.
	stageDir, err := os.MkdirTemp(filepath.Join(m.root, session), ".rewind-")
	if err != nil {
		return result, fmt.Errorf("checkpoint: create rewind staging area: %w", err)
	}
	if err := os.Chmod(stageDir, 0o700); err != nil {
		_ = os.RemoveAll(stageDir)
		return result, fmt.Errorf("checkpoint: secure rewind staging area: %w", err)
	}
	result.RewindID = filepath.Base(stageDir)
	journal := rewindJournal{
		ID: result.RewindID, Session: session, Phase: "prepared", Target: target, Message: result.UserMessage,
		Turns: append([]string(nil), turns[idx:]...), Changes: append([]RollbackChange(nil), result.Changes...),
	}
	rollbackDir := filepath.Join(stageDir, rewindRollbackDir)
	if err := secureDir(rollbackDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return result, fmt.Errorf("checkpoint: create rewind rollback store: %w", err)
	}
	for _, path := range paths {
		state, stateErr := readFileState(path)
		if stateErr != nil {
			_ = os.RemoveAll(stageDir)
			return result, stateErr
		}
		rollback := rewindRollback{Path: path, Exists: state.exists, Mode: uint32(state.mode.Perm())}
		if state.exists {
			rollback.Snapshot = snapshotName(path)
			rollback.Digest = contentDigest(state.data)
			if err := writePrivateFile(filepath.Join(rollbackDir, rollback.Snapshot), state.data); err != nil {
				_ = os.RemoveAll(stageDir)
				return result, fmt.Errorf("checkpoint: write rewind rollback for %s: %w", path, err)
			}
			if err := syncPath(filepath.Join(rollbackDir, rollback.Snapshot)); err != nil {
				_ = os.RemoveAll(stageDir)
				return result, fmt.Errorf("checkpoint: sync rewind rollback for %s: %w", path, err)
			}
		}
		journal.Files = append(journal.Files, rollback)
	}
	if err := syncPath(rollbackDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return result, fmt.Errorf("checkpoint: sync rewind rollback directory: %w", err)
	}
	if err := m.writeRewindJournal(stageDir, journal); err != nil {
		_ = os.RemoveAll(stageDir)
		return result, err
	}
	for _, turn := range turns[idx:] {
		if err := os.Rename(m.turnDir(session, turn), filepath.Join(stageDir, turn)); err != nil {
			restoreErr := m.rollbackRewind(stageDir, journal)
			return result, errors.Join(fmt.Errorf("checkpoint: stage rewound turn %s: %w", turn, err), restoreErr)
		}
	}
	restoreStagedTurns := func() error {
		return m.rollbackRewind(stageDir, journal)
	}
	if err := syncPath(stageDir); err != nil {
		return result, errors.Join(fmt.Errorf("checkpoint: sync staged turn destination: %w", err), restoreStagedTurns())
	}
	if err := syncPath(filepath.Join(m.root, session)); err != nil {
		return result, errors.Join(fmt.Errorf("checkpoint: sync staged turns: %w", err), restoreStagedTurns())
	}
	journal.Phase = "staged"
	if err := m.writeRewindJournal(stageDir, journal); err != nil {
		return result, errors.Join(err, restoreStagedTurns())
	}
	for _, path := range paths {
		plan := plans[path]
		state := fileState{exists: plan.exists, mode: plan.mode}
		if plan.exists {
			data, readErr := readAuthenticatedFile(filepath.Join(stageDir, plan.turn, plan.snapshot), plan.digest)
			if readErr != nil {
				rollbackErr := restoreStagedTurns()
				cause := errors.Join(fmt.Errorf("checkpoint: read staged snapshot for %s: %w", path, readErr), rollbackErr)
				if rollbackErr != nil {
					result.Changes = rollbackChangesForPaths(paths, plans)
					return result, &PartialRewindError{Cause: cause, Changes: result.Changes}
				}
				return result, cause
			}
			state.data = data
		}
		if err := applyFileState(path, state); err != nil {
			rollbackErr := restoreStagedTurns()
			if rollbackErr != nil {
				result.Changes = rollbackChangesForPaths(paths, plans)
				cause := errors.Join(fmt.Errorf("checkpoint: apply rewind to %s: %w", path, err), rollbackErr)
				return result, &PartialRewindError{Cause: cause, Changes: result.Changes}
			}
			return result, fmt.Errorf("checkpoint: apply rewind to %s: %w", path, err)
		}
	}
	journal.Phase = "committed"
	if err := m.writeRewindJournal(stageDir, journal); err != nil {
		rollbackErr := restoreStagedTurns()
		if rollbackErr != nil {
			result.Changes = rollbackChangesForPaths(paths, plans)
			return result, &PartialRewindError{Cause: errors.Join(err, rollbackErr), Changes: result.Changes}
		}
		return result, err
	}
	m.turns[session] = append([]string(nil), turns[:idx]...)
	m.activeSession = ""
	m.active = manifest{}
	m.inFlight = nil
	m.registerPending(session, stageDir, result)
	return result, nil
}

func messagePreview(message string) string {
	var preview strings.Builder
	preview.Grow(min(len(message), maxMessageRunes))
	runeCount := 0
	pendingSpace := false
	truncated := false
	for _, r := range message {
		if unicode.IsSpace(r) {
			pendingSpace = runeCount > 0
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if pendingSpace {
			if runeCount == maxMessageRunes {
				truncated = true
				break
			}
			preview.WriteByte(' ')
			runeCount++
			pendingSpace = false
		}
		if runeCount == maxMessageRunes {
			truncated = true
			break
		}
		preview.WriteRune(r)
		runeCount++
	}
	if !truncated {
		return preview.String()
	}
	runes := []rune(preview.String())
	return string(runes[:maxMessageRunes-1]) + "…"
}

func (m *Manager) Delete(session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validID(session) {
		return fmt.Errorf("checkpoint: invalid session %q", session)
	}
	delete(m.turns, session)
	delete(m.next, session)
	delete(m.pending, session)
	if m.activeSession == session {
		m.activeSession = ""
		m.active = manifest{}
		m.inFlight = nil
	}
	return os.RemoveAll(filepath.Join(m.root, session))
}

func (m *Manager) changed(entry entry) (bool, ChangeKind, error) {
	data, err := os.ReadFile(entry.Path)
	if entry.Before == "absent" {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		if err != nil {
			return false, "", fmt.Errorf("checkpoint: inspect result %s: %w", entry.Path, err)
		}
		return true, ChangeCreated, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return true, ChangeDeleted, nil
	}
	if err != nil {
		return false, "", fmt.Errorf("checkpoint: inspect result %s: %w", entry.Path, err)
	}
	before, err := os.ReadFile(filepath.Join(m.activeDir(), entry.Snapshot))
	if err != nil {
		return false, "", fmt.Errorf("checkpoint: read snapshot %s: %w", entry.Path, err)
	}
	info, err := os.Stat(entry.Path)
	if err != nil {
		return false, "", fmt.Errorf("checkpoint: stat result %s: %w", entry.Path, err)
	}
	return !bytes.Equal(data, before) || uint32(info.Mode().Perm()) != entry.Mode, ChangeModified, nil
}

type fileState struct {
	exists bool
	data   []byte
	mode   os.FileMode
}

type plannedRestore struct {
	exists   bool
	mode     os.FileMode
	turn     string
	snapshot string
	digest   string
	action   string
}

type rewindRollback struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Snapshot string `json:"snapshot,omitempty"`
	Digest   string `json:"snapshot_sha256,omitempty"`
	Mode     uint32 `json:"mode,omitempty"`
}

type rewindJournal struct {
	ID      string           `json:"id"`
	Session string           `json:"session"`
	Phase   string           `json:"phase"`
	Target  string           `json:"target"`
	Message string           `json:"message,omitempty"`
	Turns   []string         `json:"turns"`
	Files   []rewindRollback `json:"files"`
	Changes []RollbackChange `json:"changes"`
	MAC     string           `json:"mac,omitempty"`
}

type pendingRewind struct {
	result   Result
	stageDir string
}

func readFileState(path string) (fileState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileState{}, nil
	}
	if err != nil {
		return fileState{}, fmt.Errorf("checkpoint: inspect current file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fileState{}, fmt.Errorf("checkpoint: unsupported non-file target %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileState{}, fmt.Errorf("checkpoint: read current file %s: %w", path, err)
	}
	return fileState{exists: true, data: data, mode: info.Mode().Perm()}, nil
}

func readAuthenticatedFile(path, digest string) ([]byte, error) {
	data, err := readStableRegularFile(path)
	if err != nil {
		return nil, err
	}
	if contentDigest(data) != digest {
		return nil, fmt.Errorf("content digest mismatch")
	}
	return data, nil
}

func readStableRegularFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, fmt.Errorf("file changed while opening")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func contentDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func applyFileState(path string, state fileState) error {
	if !state.exists {
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		return syncPath(filepath.Dir(path))
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".nekocode-rewind-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err = tmp.Write(state.data); err == nil {
		err = tmp.Chmod(state.mode.Perm())
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err == nil {
		err = syncPath(parent)
	}
	return err
}

func rollbackChangesForPaths(paths []string, plans map[string]plannedRestore) []RollbackChange {
	changes := make([]RollbackChange, 0, len(paths))
	for _, path := range paths {
		changes = append(changes, RollbackChange{Path: path, Action: plans[path].action})
	}
	return changes
}

func (m *Manager) ensureRecovered(session string) error {
	if !validID(session) {
		return fmt.Errorf("checkpoint: invalid session %q", session)
	}
	matches, err := filepath.Glob(filepath.Join(m.root, session, ".rewind-*"))
	if err != nil {
		return fmt.Errorf("checkpoint: find interrupted rewinds: %w", err)
	}
	for _, stageDir := range matches {
		journal, readErr := m.readRewindJournal(stageDir)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				if err := os.RemoveAll(stageDir); err != nil {
					return fmt.Errorf("checkpoint: remove abandoned rewind staging area: %w", err)
				}
				continue
			}
			return readErr
		}
		if err := validateRewindJournal(journal, session); err != nil {
			return err
		}
		if journal.ID != filepath.Base(stageDir) {
			return fmt.Errorf("checkpoint: rewind journal id %q does not match staging directory", journal.ID)
		}
		if journal.Phase == "committed" {
			m.registerPending(session, stageDir, Result{
				RewindID: journal.ID, Turn: journal.Target, UserMessage: journal.Message,
				Changes: append([]RollbackChange(nil), journal.Changes...),
			})
			continue
		}
		if journal.Phase == "acknowledged" {
			if err := os.RemoveAll(stageDir); err != nil {
				return fmt.Errorf("checkpoint: clean acknowledged rewind %s: %w", stageDir, err)
			}
			if err := syncPath(filepath.Join(m.root, session)); err != nil {
				return fmt.Errorf("checkpoint: sync acknowledged rewind cleanup: %w", err)
			}
			if m.pending[session] != nil {
				delete(m.pending[session], journal.ID)
			}
			continue
		}
		if err := m.rollbackRewind(stageDir, journal); err != nil {
			return fmt.Errorf("checkpoint: recover interrupted rewind: %w", err)
		}
	}
	return nil
}

func (m *Manager) registerPending(session, stageDir string, result Result) {
	if m.pending[session] == nil {
		m.pending[session] = make(map[string]pendingRewind)
	}
	m.pending[session][result.RewindID] = pendingRewind{result: result, stageDir: stageDir}
}

// Recovered returns committed rewinds whose conversation event still needs to
// be persisted by the session layer.
func (m *Manager) Recovered(session string) []Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.pending[session]))
	for id := range m.pending[session] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		results = append(results, m.pending[session][id].result)
	}
	return results
}

// AcknowledgeRecovered marks the conversation event durable before deleting
// the crash-recovery evidence.
func (m *Manager) AcknowledgeRecovered(session, rewindID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending, ok := m.pending[session][rewindID]
	if !ok {
		return nil
	}
	journal, err := m.readRewindJournal(pending.stageDir)
	if err != nil {
		return err
	}
	journal.Phase = "acknowledged"
	if err := m.writeRewindJournal(pending.stageDir, journal); err != nil {
		return err
	}
	if err := os.RemoveAll(pending.stageDir); err != nil {
		return fmt.Errorf("checkpoint: clean acknowledged rewind: %w", err)
	}
	if err := syncPath(filepath.Join(m.root, session)); err != nil {
		return fmt.Errorf("checkpoint: sync acknowledged rewind cleanup: %w", err)
	}
	delete(m.pending[session], rewindID)
	if len(m.pending[session]) == 0 {
		delete(m.pending, session)
	}
	return nil
}

func (m *Manager) rollbackRewind(stageDir string, journal rewindJournal) error {
	var rollbackErr error
	if journal.Phase != "prepared" {
		for _, item := range journal.Files {
			state := fileState{exists: item.Exists, mode: os.FileMode(item.Mode)}
			if item.Exists {
				data, err := readAuthenticatedFile(filepath.Join(stageDir, rewindRollbackDir, item.Snapshot), item.Digest)
				if err != nil {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("read rollback for %s: %w", item.Path, err))
					continue
				}
				state.data = data
			}
			if err := applyFileState(item.Path, state); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore %s: %w", item.Path, err))
			}
		}
	}
	for idx := len(journal.Turns) - 1; idx >= 0; idx-- {
		turn := journal.Turns[idx]
		staged := filepath.Join(stageDir, turn)
		_, stagedErr := os.Stat(staged)
		_, targetErr := os.Stat(m.turnDir(journal.Session, turn))
		switch {
		case errors.Is(stagedErr, os.ErrNotExist):
			// This turn was not moved before the interruption, or was restored by
			// an earlier recovery attempt.
			continue
		case stagedErr != nil:
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("inspect staged turn %s: %w", turn, stagedErr))
		case targetErr == nil:
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore turn %s: destination already exists", turn))
		case !errors.Is(targetErr, os.ErrNotExist):
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("inspect turn %s: %w", turn, targetErr))
		default:
			if err := os.Rename(staged, m.turnDir(journal.Session, turn)); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore turn %s: %w", turn, err))
			}
		}
	}
	if rollbackErr != nil {
		return rollbackErr
	}
	if err := syncPath(stageDir); err != nil {
		return fmt.Errorf("sync rewind staging source: %w", err)
	}
	sessionDir := filepath.Join(m.root, journal.Session)
	if err := syncPath(sessionDir); err != nil {
		return fmt.Errorf("sync restored checkpoint turns: %w", err)
	}
	if err := os.RemoveAll(stageDir); err != nil {
		return fmt.Errorf("remove rewind staging area: %w", err)
	}
	if err := syncPath(sessionDir); err != nil {
		return fmt.Errorf("sync rewind staging cleanup: %w", err)
	}
	return nil
}

func (m *Manager) activeDir() string { return m.turnDir(m.activeSession, m.active.Turn) }

func (m *Manager) turnDir(session, turn string) string {
	return filepath.Join(m.root, session, turn)
}

func (m *Manager) existingTurns(session string, turns []string) []string {
	existing := make([]string, 0, len(turns))
	for _, turn := range turns {
		_, err := m.readManifest(m.turnDir(session, turn))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			// Keep the index entry so History/Rewind can surface authentication or
			// migration failures instead of silently persisting an empty index.
			existing = append(existing, turn)
			continue
		}
		existing = append(existing, turn)
	}
	return existing
}

func snapshotName(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:16]) + ".bin"
}

func secureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func writePrivateFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (m *Manager) writeManifest(dir string, value manifest) error {
	key, err := m.authKey()
	if err != nil {
		return err
	}
	return writeManifestWithKey(dir, value, key)
}

func writeManifestWithKey(dir string, value manifest, key []byte) error {
	value.Version = 2
	value.MAC = ""
	mac, err := manifestMAC(key, value)
	if err != nil {
		return err
	}
	value.MAC = mac
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".manifest-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err = file.Chmod(0o600); err == nil {
		var written int
		written, err = file.Write(data)
		if err == nil && written != len(data) {
			err = fmt.Errorf("short manifest write: %d of %d bytes", written, len(data))
		}
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, manifestName)); err != nil {
		return err
	}
	return syncPath(dir)
}

func (m *Manager) readManifest(dir string) (manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return manifest{}, fmt.Errorf("checkpoint: read manifest %s: %w", dir, err)
	}
	var value manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return manifest{}, fmt.Errorf("checkpoint: decode manifest %s: %w", dir, err)
	}
	key, err := m.authKey()
	if err != nil {
		return manifest{}, err
	}
	provided := value.MAC
	value.MAC = ""
	if provided == "" {
		return manifest{}, fmt.Errorf("checkpoint: manifest authentication failed for %s", dir)
	}
	expected, err := manifestMAC(key, value)
	if err != nil {
		return manifest{}, err
	}
	if provided == "" || !hmac.Equal([]byte(provided), []byte(expected)) {
		return manifest{}, fmt.Errorf("checkpoint: manifest authentication failed for %s", dir)
	}
	if value.Version != 2 {
		return manifest{}, fmt.Errorf("checkpoint: unsupported authenticated manifest version %d", value.Version)
	}
	value.MAC = provided
	return value, nil
}

func migrateLegacyManifestWithKey(dir string, value *manifest, key []byte) error {
	expectedSession := filepath.Base(filepath.Dir(dir))
	expectedTurn := filepath.Base(dir)
	value.Session = expectedSession
	for idx := range value.Entries {
		item := &value.Entries[idx]
		if item.Before != "file" {
			continue
		}
		if item.Snapshot != snapshotName(item.Path) || filepath.Base(item.Snapshot) != item.Snapshot {
			return fmt.Errorf("invalid snapshot for %s", item.Path)
		}
		data, err := readStableRegularFile(filepath.Join(dir, item.Snapshot))
		if err != nil {
			return fmt.Errorf("read snapshot for %s: %w", item.Path, err)
		}
		item.Digest = contentDigest(data)
	}
	if err := validateManifest(*value, expectedSession, expectedTurn); err != nil {
		return err
	}
	value.Version = 2
	if err := writeManifestWithKey(dir, *value, key); err != nil {
		return err
	}
	return nil
}

func (m *Manager) writeRewindJournal(dir string, value rewindJournal) error {
	key, err := m.authKey()
	if err != nil {
		return err
	}
	value.MAC = ""
	value.MAC, err = rewindJournalMAC(key, value)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint: encode rewind journal: %w", err)
	}
	tmp := filepath.Join(dir, ".rewind.tmp")
	if err := writePrivateFile(tmp, data); err != nil {
		return fmt.Errorf("checkpoint: write rewind journal: %w", err)
	}
	path := filepath.Join(dir, rewindJournalName)
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("checkpoint: commit rewind journal: %w", err)
	}
	if err := syncPath(path); err != nil {
		return fmt.Errorf("checkpoint: sync rewind journal: %w", err)
	}
	if err := syncPath(dir); err != nil {
		return fmt.Errorf("checkpoint: sync rewind directory: %w", err)
	}
	return nil
}

func (m *Manager) readRewindJournal(dir string) (rewindJournal, error) {
	data, err := os.ReadFile(filepath.Join(dir, rewindJournalName))
	if err != nil {
		return rewindJournal{}, fmt.Errorf("checkpoint: read rewind journal %s: %w", dir, err)
	}
	var value rewindJournal
	if err := json.Unmarshal(data, &value); err != nil {
		return rewindJournal{}, fmt.Errorf("checkpoint: decode rewind journal %s: %w", dir, err)
	}
	key, err := m.authKey()
	if err != nil {
		return rewindJournal{}, err
	}
	provided := value.MAC
	value.MAC = ""
	expected, err := rewindJournalMAC(key, value)
	if err != nil {
		return rewindJournal{}, err
	}
	if provided == "" || !hmac.Equal([]byte(provided), []byte(expected)) {
		return rewindJournal{}, fmt.Errorf("checkpoint: rewind journal authentication failed for %s", dir)
	}
	value.MAC = provided
	return value, nil
}

func (m *Manager) authKey() ([]byte, error) {
	if m.macKey != nil {
		return m.macKey, nil
	}
	key, err := m.acquireAuthKey()
	if err != nil {
		return nil, fmt.Errorf("checkpoint: load authentication key: %w", err)
	}
	m.macKey = key
	return m.macKey, nil
}

// acquireAuthKey loads or publishes the root authentication key. Concurrent
// managers race to publish the pending key and link it into place, and a
// loser can observe the pending file vanish once the winner finishes, so the
// whole acquisition is retried before giving up.
func (m *Manager) acquireAuthKey() ([]byte, error) {
	if err := secureDir(m.root); err != nil {
		return nil, fmt.Errorf("checkpoint: secure authentication root: %w", err)
	}
	finalPath := filepath.Join(m.root, macKeyName)
	pendingPath := filepath.Join(m.root, macPendingKeyName)
	for attempt := 0; attempt < 5; attempt++ {
		key, err := readAuthKey(finalPath)
		if err == nil {
			return key, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		key, err = readAuthKey(pendingPath)
		if err == nil {
			return m.adoptPendingKey(key)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		key = make([]byte, sha256.Size)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err := publishAuthKey(m.root, pendingPath, key); err != nil {
			if errors.Is(err, os.ErrExist) {
				// Another manager published its pending key first; retry from
				// the published files instead of failing on the vanished
				// pending path.
				continue
			}
			return nil, err
		}
		return m.adoptPendingKey(key)
	}
	return nil, fmt.Errorf("authentication key publication kept racing with concurrent managers")
}

// adoptPendingKey migrates legacy manifests with the pending key and links the
// pending file into its final location. It returns the key that ended up
// published, which may differ from the input when another manager won the race.
func (m *Manager) adoptPendingKey(key []byte) ([]byte, error) {
	finalPath := filepath.Join(m.root, macKeyName)
	pendingPath := filepath.Join(m.root, macPendingKeyName)
	// The final key may have been published between our checks; prefer it so
	// legacy manifests are never migrated with a stale key.
	published, err := readAuthKey(finalPath)
	if err == nil {
		_ = os.Remove(pendingPath)
		return published, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := migrateAllLegacyManifests(m.root, key); err != nil {
		return nil, err
	}
	if err := os.Link(pendingPath, finalPath); err != nil {
		published, readErr := readAuthKey(finalPath)
		if readErr != nil {
			return nil, errors.Join(fmt.Errorf("link authentication key: %w", err), readErr)
		}
		if !hmac.Equal(published, key) {
			return nil, errors.Join(fmt.Errorf("concurrent authentication key mismatch"), err)
		}
		key = published
	}
	if err := syncPath(m.root); err != nil {
		return nil, err
	}
	_ = os.Remove(pendingPath)
	return key, syncPath(m.root)
}

func readAuthKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("authentication key is not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("authentication key permissions are %o, want 600", info.Mode().Perm())
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != sha256.Size {
		return nil, fmt.Errorf("authentication key has invalid length %d", len(key))
	}
	return key, nil
}

func publishAuthKey(root, target string, key []byte) error {
	file, err := os.CreateTemp(root, ".auth-key-*")
	if err != nil {
		return err
	}
	tmpName := file.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err = file.Chmod(0o600); err == nil {
		var written int
		written, err = file.Write(key)
		if err == nil && written != len(key) {
			err = fmt.Errorf("short authentication key write: %d of %d bytes", written, len(key))
		}
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = os.Link(tmpName, target)
	}
	if err == nil {
		err = syncPath(root)
	}
	return err
}

func migrateAllLegacyManifests(root string, key []byte) error {
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", manifestName))
	if err != nil {
		return err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value manifest
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("decode legacy manifest %s: %w", path, err)
		}
		dir := filepath.Dir(path)
		if value.MAC != "" {
			provided := value.MAC
			value.MAC = ""
			expected, err := manifestMAC(key, value)
			if err != nil || !hmac.Equal([]byte(provided), []byte(expected)) || value.Version != 2 {
				return fmt.Errorf("authenticated manifest validation failed for %s", path)
			}
			continue
		}
		if value.Version != 0 {
			return fmt.Errorf("unsigned manifest has non-legacy version %d: %s", value.Version, path)
		}
		if err := migrateLegacyManifestWithKey(dir, &value, key); err != nil {
			return fmt.Errorf("migrate legacy manifest %s: %w", path, err)
		}
	}
	return nil
}

func manifestMAC(key []byte, value manifest) (string, error) {
	value.MAC = ""
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func rewindJournalMAC(key []byte, value rewindJournal) (string, error) {
	value.MAC = ""
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func validateRewindJournal(value rewindJournal, session string) error {
	if value.Session != session || !validID(value.Session) {
		return fmt.Errorf("checkpoint: invalid rewind journal session %q", value.Session)
	}
	if !validID(value.ID) {
		return fmt.Errorf("checkpoint: invalid rewind journal id %q", value.ID)
	}
	switch value.Phase {
	case "prepared", "staged", "committed", "acknowledged":
	default:
		return fmt.Errorf("checkpoint: invalid rewind journal phase %q", value.Phase)
	}
	seenTurns := make(map[string]struct{}, len(value.Turns))
	for _, turn := range value.Turns {
		if !validID(turn) {
			return fmt.Errorf("checkpoint: invalid rewind journal turn %q", turn)
		}
		if _, exists := seenTurns[turn]; exists {
			return fmt.Errorf("checkpoint: duplicate rewind journal turn %q", turn)
		}
		seenTurns[turn] = struct{}{}
	}
	if len(value.Turns) == 0 || value.Target != value.Turns[0] {
		return fmt.Errorf("checkpoint: rewind journal target %q does not match consumed turns", value.Target)
	}
	seenPaths := make(map[string]struct{}, len(value.Files))
	for _, item := range value.Files {
		if item.Path == "" || !filepath.IsAbs(item.Path) || filepath.Clean(item.Path) != item.Path {
			return fmt.Errorf("checkpoint: invalid rewind rollback path %q", item.Path)
		}
		if _, exists := seenPaths[item.Path]; exists {
			return fmt.Errorf("checkpoint: duplicate rewind rollback path %s", item.Path)
		}
		seenPaths[item.Path] = struct{}{}
		if item.Mode&^0o777 != 0 {
			return fmt.Errorf("checkpoint: invalid rewind rollback mode %#o for %s", item.Mode, item.Path)
		}
		if item.Exists {
			if item.Snapshot != snapshotName(item.Path) || filepath.Base(item.Snapshot) != item.Snapshot {
				return fmt.Errorf("checkpoint: invalid rewind rollback snapshot for %s", item.Path)
			}
			if !validContentDigest(item.Digest) {
				return fmt.Errorf("checkpoint: invalid rewind rollback digest for %s", item.Path)
			}
		} else if item.Snapshot != "" || item.Digest != "" {
			return fmt.Errorf("checkpoint: unexpected rewind rollback snapshot for %s", item.Path)
		}
	}
	seenChanges := make(map[string]struct{}, len(value.Changes))
	for _, change := range value.Changes {
		if _, exists := seenPaths[change.Path]; !exists {
			return fmt.Errorf("checkpoint: rewind change path %q has no rollback record", change.Path)
		}
		if _, exists := seenChanges[change.Path]; exists {
			return fmt.Errorf("checkpoint: duplicate rewind change path %s", change.Path)
		}
		seenChanges[change.Path] = struct{}{}
		switch change.Action {
		case RollbackRemovedCreatedFile, RollbackRestoredFile, RollbackRestoredDeletedFile:
		default:
			return fmt.Errorf("checkpoint: invalid rewind action %q for %s", change.Action, change.Path)
		}
	}
	if len(seenChanges) != len(seenPaths) {
		return fmt.Errorf("checkpoint: rewind journal changes do not cover every rollback path")
	}
	return nil
}

func syncPath(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	return errors.Join(syncErr, file.Close())
}

func validateManifest(value manifest, expectedSession, expectedTurn string) error {
	if value.Session != expectedSession || !validID(value.Session) {
		return fmt.Errorf("checkpoint: manifest session %q does not match %q", value.Session, expectedSession)
	}
	if value.Turn != expectedTurn || !validID(value.Turn) {
		return fmt.Errorf("checkpoint: manifest turn %q does not match %q", value.Turn, expectedTurn)
	}
	seen := make(map[string]struct{}, len(value.Entries))
	for _, item := range value.Entries {
		if item.Path == "" || !filepath.IsAbs(item.Path) || filepath.Clean(item.Path) != item.Path {
			return fmt.Errorf("checkpoint: invalid manifest path %q", item.Path)
		}
		if _, exists := seen[item.Path]; exists {
			return fmt.Errorf("checkpoint: duplicate manifest path %s", item.Path)
		}
		seen[item.Path] = struct{}{}
		if item.Mode&^0o777 != 0 {
			return fmt.Errorf("checkpoint: invalid mode %#o for %s", item.Mode, item.Path)
		}
		switch item.Change {
		case ChangeCreated, ChangeModified, ChangeDeleted:
		default:
			return fmt.Errorf("checkpoint: invalid change %q for %s", item.Change, item.Path)
		}
		switch item.Before {
		case "absent":
			if item.Snapshot != "" || item.Digest != "" || item.Change != ChangeCreated {
				return fmt.Errorf("checkpoint: invalid absent state for %s", item.Path)
			}
		case "file":
			if item.Snapshot != snapshotName(item.Path) || filepath.Base(item.Snapshot) != item.Snapshot {
				return fmt.Errorf("checkpoint: invalid snapshot for %s", item.Path)
			}
			if !validContentDigest(item.Digest) {
				return fmt.Errorf("checkpoint: invalid snapshot digest for %s", item.Path)
			}
			if item.Change != ChangeModified && item.Change != ChangeDeleted {
				return fmt.Errorf("checkpoint: invalid file change %q for %s", item.Change, item.Path)
			}
		default:
			return fmt.Errorf("checkpoint: invalid state %q for %s", item.Before, item.Path)
		}
	}
	return nil
}

func validContentDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validID(value string) bool {
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func cleanTurns(turns []string) []string {
	out := make([]string, 0, len(turns))
	for _, turn := range turns {
		if validID(turn) && indexOf(out, turn) < 0 {
			out = append(out, turn)
		}
	}
	return out
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}

func highestTurn(turns []string) int {
	highest := 0
	for _, turn := range turns {
		if value, err := strconv.Atoi(turn); err == nil && value > highest {
			highest = value
		}
	}
	return highest
}
