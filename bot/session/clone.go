package session

import (
	"encoding/json"
	"reflect"
	"slices"

	"nekocode/bot/policy/ledger"
	"nekocode/bot/provider/types"
)

// Clone separates all mutable session data, including the transcript omitted
// from session.json. JSON round-tripping the entire snapshot would lose it.
func (s *Snapshot) Clone() *Snapshot {
	if s == nil {
		return nil
	}
	out := *s
	out.Messages = cloneMessages(s.Messages)
	out.Transcript = cloneMessages(s.Transcript)
	out.LoadedSkills = slices.Clone(s.LoadedSkills)
	out.CheckpointTurns = slices.Clone(s.CheckpointTurns)
	out.Ledger.ReadFiles = slices.Clone(s.Ledger.ReadFiles)
	out.Ledger.ModifiedFiles = slices.Clone(s.Ledger.ModifiedFiles)
	out.Ledger.BlockedTools = cloneToolEvents(s.Ledger.BlockedTools)
	out.Ledger.ToolErrors = cloneToolEvents(s.Ledger.ToolErrors)
	if s.PrefixTurn != nil {
		stats := *s.PrefixTurn
		stats.PeakMiss.Parts = slices.Clone(stats.PeakMiss.Parts)
		stats.LowestHit.Parts = slices.Clone(stats.LowestHit.Parts)
		out.PrefixTurn = &stats
	}
	return &out
}

func cloneMessages(messages []types.Message) []types.Message {
	out := slices.Clone(messages)
	for i := range out {
		out[i].ToolCalls = slices.Clone(out[i].ToolCalls)
		out[i].Images = slices.Clone(out[i].Images)
	}
	return out
}

func cloneToolEvents(events []ledger.ToolEvent) []ledger.ToolEvent {
	out := slices.Clone(events)
	for i := range out {
		if out[i].Args != nil {
			out[i].Args = cloneValue(out[i].Args).(map[string]any)
		}
	}
	return out
}

// Tool arguments are JSON values; preserve numeric types rather than encode
// and decode them merely to copy their maps and arrays.
func cloneValue(value any) any {
	if value == nil {
		return nil
	}
	switch value := value.(type) {
	case map[string]any:
		if value == nil {
			return map[string]any(nil)
		}
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = cloneValue(v)
		}
		return out
	case []any:
		out := slices.Clone(value)
		for i := range out {
			out[i] = cloneValue(out[i])
		}
		return out
	case []string:
		return slices.Clone(value)
	case json.RawMessage:
		return slices.Clone(value)
	case []byte:
		return slices.Clone(value)
	default:
		return cloneComposite(reflect.ValueOf(value)).Interface()
	}
}

// Args normally contains JSON-decoded map[string]any and []any values. Copy
// other exported slice/map shapes too, so programmatic callers cannot leave
// aliases inside an otherwise detached session snapshot.
func cloneComposite(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(cloneComposite(value.Elem()))
		return out
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(cloneComposite(value.Elem()))
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneComposite(iter.Value()))
		}
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneComposite(value.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneComposite(value.Index(i)))
		}
		return out
	case reflect.Struct:
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		for i := 0; i < value.NumField(); i++ {
			if out.Field(i).CanSet() && value.Field(i).CanInterface() {
				out.Field(i).Set(cloneComposite(value.Field(i)))
			}
		}
		return out
	default:
		return value
	}
}
