// Package refbridge carries bounded reference queries without exposing session
// storage or granting file access to tools and context projection.
package refbridge

import "context"

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
	SessionID string      `json:"session_id"`
	Items     []Reference `json:"items"`
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
	ID     string
	Before int
	Limit  int
}
type Reader func(context.Context, Query) (Page, error)
