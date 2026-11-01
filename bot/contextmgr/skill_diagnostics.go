package contextmgr

import (
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
)

// Derive diagnostics from the final request, never LoadedSkills or text tags.
// The ordered visible content is the evidence; no skill registry is copied.
func skillDeliveryDigest(messages []types.Message) ([32]byte, int) {
	type delivery struct {
		Role    string `json:"role"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	deliveries := []delivery{}
	for _, message := range messages {
		if message.IsError || message.Source == core.DeferredOutputSource || message.SkillDelivery.Schema != 0 && message.SkillDelivery.State != types.SkillReady {
			continue
		}
		explicitWorkflow := message.Role == "user" && message.Source == types.MessageSourceSkillWorkflow
		toolDelivery := message.Role == "tool" && message.SkillName != ""
		if explicitWorkflow || toolDelivery {
			deliveries = append(deliveries, delivery{message.Role, message.SkillName, message.Content})
		}
	}
	return digestJSON(struct {
		Schema     int        `json:"schema"`
		Deliveries []delivery `json:"deliveries"`
	}{1, deliveries}), len(deliveries)
}
