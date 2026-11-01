package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/provider/types"
)

// Only schema-backed admissions qualify. Legacy SkillName, LoadedSkills and
// workflow-looking body text never establish complete admission on their own.
func skillReference(sessionID string, sequence int, msg types.Message) (refbridge.Reference, bool) {
	d := msg.SkillDelivery
	if d.Schema != 1 || d.OriginSequence < 1 || d.OriginSequence > sequence || d.State != types.SkillReady || d.Name == "" || msg.IsError || len(d.ContentHash) != 71 || d.ContentHash[:7] != "sha256:" {
		return refbridge.Reference{}, false
	}
	if _, err := hex.DecodeString(d.ContentHash[7:]); err != nil {
		return refbridge.Reference{}, false
	}
	switch d.Source {
	case "tool":
		if !(msg.Role == "tool" && msg.ToolOutput.Path == "" && !msg.ToolOutput.Incomplete || msg.Role == "user" && msg.Source == types.MessageSourceRuntimeEvent) {
			return refbridge.Reference{}, false
		}
	case "command", "selected":
		if msg.Role != "user" || msg.Source != types.MessageSourceSkillWorkflow {
			return refbridge.Reference{}, false
		}
	case "delegated":
		if msg.Role != "user" || msg.Source != types.MessageSourceRuntimeEvent {
			return refbridge.Reference{}, false
		}
	default:
		return refbridge.Reference{}, false
	}
	data, _ := json.Marshal(struct {
		Session  string
		Sequence int
		Delivery types.SkillDelivery
	}{sessionID, sequence, d})
	digest := sha256.Sum256(data)
	return refbridge.Reference{Kind: "skill", ID: fmt.Sprintf("skill.%d.%s", sequence, hex.EncodeToString(digest[:])), Sequence: sequence, Name: d.Name, Source: d.Source, ContentHash: d.ContentHash}, true
}
