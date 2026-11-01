// Package refbridge carries bounded reference queries without exposing session
// storage or granting file access to tools and context projection.
package refbridge

import (
	"context"

	"nekocode/protocol"
)

const MaxPageRecords = 20

type Reference struct {
	Kind        string `json:"kind,omitempty"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	ID          string `json:"id"`
	Sequence    int    `json:"sequence"`
	Incomplete  bool   `json:"incomplete,omitempty"`
}

type Page struct {
	SessionID string         `json:"session_id"`
	Items     []Reference    `json:"items"`
	Evidence  []EvidenceItem `json:"evidence,omitempty"`
	// Before is an exclusive raw history cursor, not an item offset.
	Before      int  `json:"before"`
	HasMore     bool `json:"has_more"`
	Scanned     int  `json:"scanned"`
	Unavailable bool `json:"unavailable,omitempty"`
	// Inspect reports the reference location, never file content or approval.
	Status string `json:"status,omitempty"`
	Path   string `json:"path,omitempty"`
}

type Query struct {
	// Kind is host-selected, never a caller-supplied session or actor.
	Kind      string
	ID        string
	Before    int
	Limit     int
	TaskID    string
	AttemptID string
}
type Reader func(context.Context, Query) (Page, error)

type readerKey struct{}

// WithReader binds a query to one execution without mutating shared tools.
// A nil reader explicitly masks an inherited actor's reader.
func WithReader(ctx context.Context, read Reader) context.Context {
	return context.WithValue(ctx, readerKey{}, read)
}
func ReaderFromContext(ctx context.Context) (Reader, bool) {
	read, ok := ctx.Value(readerKey{}).(Reader)
	return read, ok
}

const EvidenceKind = "evidence"

// EvidenceItem is a projection; Detail is present only on inspect and never
// includes artifact paths. The source remains the transcript record.
type EvidenceItem struct {
	ID          string                 `json:"id"`
	Sequence    int                    `json:"sequence"`
	ReceiptID   string                 `json:"receipt_id"`
	Tool        string                 `json:"tool"`
	Status      string                 `json:"status"`
	HasArtifact bool                   `json:"has_artifact,omitempty"`
	Detail      *protocol.ToolEvidence `json:"detail,omitempty"`
}
