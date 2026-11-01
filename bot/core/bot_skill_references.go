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
	if !refbridge.ReadySkill(msg, sequence) {
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
