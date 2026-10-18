package runtime

import (
	"encoding/json"

	"nekocode/runtime/internal/core"
)

// ValidateEventPayload checks known event payloads. Unknown event types are
// allowed so consumers may ignore future protocol additions safely.
func ValidateEventPayload(event Event) error { return core.ValidateEventPayload(event) }

// DecodeEventPayload restores the typed payload used by in-process consumers.
func DecodeEventPayload(typ EventType, data json.RawMessage) (any, error) {
	return core.DecodeEventPayload(typ, data)
}
