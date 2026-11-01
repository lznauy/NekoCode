package refbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func ArtifactReference(sessionID string, sequence int, artifact protocol.ToolOutputArtifact) Reference {
	// Hash the source location and scope, not the current file contents. This ID
	// survives compaction and never pretends to be a content-version receipt.
	data, _ := json.Marshal(struct {
		Session  string
		Sequence int
		Artifact protocol.ToolOutputArtifact
	}{sessionID, sequence, artifact})
	digest := sha256.Sum256(data)
	return Reference{Kind: "artifact", ID: fmt.Sprintf("artifact.%d.%s", sequence, hex.EncodeToString(digest[:])), Sequence: sequence, Incomplete: artifact.Incomplete}
}

func Sequence(id string) (int, error) {
	parts := strings.Split(id, ".")
	if len(parts) != 3 || (parts[0] != "artifact" && parts[0] != "skill" && parts[0] != EvidenceKind) || len(parts[2]) != 64 {
		return 0, fmt.Errorf("invalid reference ID")
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return 0, fmt.Errorf("invalid reference ID")
	}
	seq, err := strconv.Atoi(parts[1])
	if err != nil || seq < 1 || strconv.Itoa(seq) != parts[1] || seq == int(^uint(0)>>1) {
		return 0, fmt.Errorf("invalid reference ID")
	}
	return seq, nil
}

// Only schema-backed admissions qualify. Legacy SkillName, LoadedSkills and
// workflow-looking body text never establish complete admission on their own.
func SkillReference(sessionID string, sequence int, msg types.Message) (Reference, bool) {
	d := msg.SkillDelivery
	if !ReadySkill(msg, sequence) {
		return Reference{}, false
	}
	data, _ := json.Marshal(struct {
		Session  string
		Sequence int
		Delivery types.SkillDelivery
	}{sessionID, sequence, d})
	digest := sha256.Sum256(data)
	return Reference{Kind: "skill", ID: fmt.Sprintf("skill.%d.%s", sequence, hex.EncodeToString(digest[:])), Sequence: sequence, Name: d.Name, Source: d.Source, ContentHash: d.ContentHash}, true
}
