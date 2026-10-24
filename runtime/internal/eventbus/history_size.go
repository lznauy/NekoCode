package eventbus

import (
	"encoding/json"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

// EstimateEventBytes is shared with the bounded, subscriber-local archive view.
func EstimateEventBytes(ev core.Event) int { return eventBytes(ev) }

// Count frequent text payloads directly. Less frequent structured controls
// use their encoded size so large questions or approval arguments count too.
func eventBytes(ev core.Event) int {
	n := 256 + len(ev.ID) + len(ev.RunID) + len(ev.Source.Kind) + len(ev.Source.ID)
	switch p := ev.Payload.(type) {
	case core.DeltaPayload:
		n += len(p.Delta)
	case core.MessagePayload:
		n += len(p.Content) + len(p.Role) + len(p.Source.Kind) + len(p.Source.ID) + len(p.Sender.ID) + len(p.Sender.Username) + len(p.Sender.Display)
		for _, image := range p.Images {
			n += 64 + len(image.Path) + len(image.Label)
		}
	case core.ToolPayload:
		n += len(p.Args) + len(p.Input) + len(p.Output) + len(p.Preview) + len(p.ToolName) + len(p.CallID) + len(p.SubAgentID) + len(p.Decision)
	case core.RunResult:
		n += len(p.Output) + len(p.Error)
	case protocol.CompactionEvent:
		n += len(p.ID) + len(p.Delta) + len(p.Summary) + len(p.Error) + len(p.Warning)
	case protocol.SubAgentOutput:
		n += len(p.ID) + len(p.Kind) + len(p.Text)
	case protocol.RunSummary:
		n += len(p.StopReason)
	case protocol.Metrics:
		n += len(p.Duration)
	case core.PhasePayload:
		n += len(p.Phase)
	default:
		if data, err := json.Marshal(p); err == nil {
			n += len(data)
		}
	}
	return n
}
