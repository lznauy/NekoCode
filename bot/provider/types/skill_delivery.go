package types

import (
	"crypto/sha256"
	"encoding/hex"
)

// SkillDelivery is local admission metadata, not provider acknowledgement.
// Ready means the host has the complete selected text available for a request.
// Zero metadata in older histories is unknown, never inferred from body tags.
type SkillDelivery struct {
	Schema         int    `json:"schema"`
	Name           string `json:"name,omitempty"`
	Source         string `json:"source"`
	State          string `json:"state"`
	ContentHash    string `json:"content_hash,omitempty"`
	OriginSequence int    `json:"origin_sequence,omitempty"`
}

const (
	SkillReady      = "ready"
	SkillDeferred   = "deferred"
	SkillFailed     = "failed"
	SkillIncomplete = "incomplete"
)

// SkillContentHash identifies the exact host-admitted workflow bytes.
func SkillContentHash(content string) string {
	digest := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(digest[:])
}
