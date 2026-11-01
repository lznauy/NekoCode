package execution

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/google/uuid"
	textutil "nekocode/util/text"
)

const maxReadEvidence = 128

type readEvidence struct {
	path     string
	version  [32]byte
	preview  [32]byte
	sequence uint64
	visible  bool
}

// ReadEvidence belongs to one executor. It retains hashes, never file content,
// and is not copied with file caches or restored from session data.
type ReadEvidence struct {
	mu       sync.Mutex
	entries  map[string]readEvidence
	sequence uint64
}

// FileReadsFromContext enables version checks for managed tool execution.
// Raw standalone tools without ExecutionState retain their low-level API;
// applications use an Executor or supply their own isolated ExecutionState.
func FileReadsFromContext(ctx context.Context) *ReadEvidence {
	if state := ExecutionStateFromContext(ctx); state != nil {
		return &state.Reads
	}
	return nil
}

// Record creates a pending, process-local read receipt. Only delivery of its
// exact preview can activate it; producer/provider call IDs are not identities.
func (r *ReadEvidence) Record(path string, data []byte, preview string) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[string]readEvidence)
	}
	if len(r.entries) >= maxReadEvidence {
		var oldest string
		for id, entry := range r.entries {
			if oldest == "" || entry.sequence < r.entries[oldest].sequence {
				oldest = id
			}
		}
		delete(r.entries, oldest)
	}
	r.sequence++
	id := uuid.NewString()
	r.entries[id] = readEvidence{path: normalizePath(path), version: sha256.Sum256(data), preview: sha256.Sum256([]byte(textutil.NormalizeTerminalOutput(preview))), sequence: r.sequence}
	return id
}

// Observe replaces authorization with the reads actually present in one model
// request. Deferred/truncated results cannot activate the original preview.
func (r *ReadEvidence) Observe(deliveries map[string]string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, entry := range r.entries {
		content, present := deliveries[id]
		entry.visible = present && sha256.Sum256([]byte(content)) == entry.preview
		r.entries[id] = entry
	}
}

// Check must run inside the same target lock as the following mutation.
func (r *ReadEvidence) Check(path string, data []byte) error {
	if r == nil {
		return nil
	}
	path = normalizePath(path)
	version := sha256.Sum256(data)
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := false
	for _, entry := range r.entries {
		if entry.path != path || !entry.visible {
			continue
		}
		seen = true
		if entry.version == version {
			return nil
		}
	}
	if seen {
		return fmt.Errorf("file changed since its delivered read: %s; read the current version before modifying it", path)
	}
	return fmt.Errorf("no current read was delivered for %s; read it in smaller ranges before modifying it", path)
}

func (r *ReadEvidence) Invalidate(path string) {
	if r == nil {
		return
	}
	path = normalizePath(path)
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, entry := range r.entries {
		if entry.path == path {
			delete(r.entries, id)
		}
	}
}

func (r *ReadEvidence) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.entries)
}
