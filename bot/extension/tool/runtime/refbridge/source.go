package refbridge

import (
	"encoding/hex"

	"nekocode/bot/provider/types"
)

// Record carries a source sequence without the display pager's replay header.
type Record struct {
	Sequence int
	Message  types.Message
}
type SourcePage struct {
	Records []Record
	Before  int
	HasMore bool
	Bytes   int
}

// ReadySkill is the shared eligibility contract for index selection and IDs.
// Legacy names, unversioned records and workflow-looking text do not qualify.
func ReadySkill(msg types.Message, sequence int) bool {
	d := msg.SkillDelivery
	if d.Schema != 1 || d.OriginSequence < 1 || d.OriginSequence > sequence || d.State != types.SkillReady || d.Name == "" || msg.IsError || len(d.ContentHash) != 71 || d.ContentHash[:7] != "sha256:" {
		return false
	}
	if _, err := hex.DecodeString(d.ContentHash[7:]); err != nil {
		return false
	}
	switch d.Source {
	case "tool":
		return msg.Role == "tool" && msg.ToolOutput.Path == "" && !msg.ToolOutput.Incomplete || msg.Role == "user" && msg.Source == types.MessageSourceRuntimeEvent
	case "command", "selected":
		return msg.Role == "user" && msg.Source == types.MessageSourceSkillWorkflow
	case "delegated":
		return msg.Role == "user" && msg.Source == types.MessageSourceRuntimeEvent
	default:
		return false
	}
}

func HasReference(msg types.Message, sequence int) bool {
	return msg.Role == "tool" && msg.ToolOutput.Path != "" || ReadySkill(msg, sequence)
}
