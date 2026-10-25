package policy

import (
	"maps"
	"reflect"
	"slices"
)

type hookMemory struct {
	ints    map[string]int64
	strings map[string]string
	once    bool
}

func (e *hookEngine) stateFor(name string, facts Facts) *hookState {
	memory := e.state[name]
	return &hookState{
		facts:   cloneFacts(facts),
		ints:    maps.Clone(memory.ints),
		strings: maps.Clone(memory.strings),
	}
}

func cloneFacts(facts Facts) Facts {
	if facts.Tool.Args != nil {
		facts.Tool.Args = cloneFactValue(reflect.ValueOf(facts.Tool.Args)).Interface().(map[string]any)
	}
	if facts.Batch != nil {
		facts.Batch = slices.Clone(facts.Batch)
		for i := range facts.Batch {
			if facts.Batch[i].Args != nil {
				facts.Batch[i].Args = cloneFactValue(reflect.ValueOf(facts.Batch[i].Args)).Interface().(map[string]any)
			}
		}
	}
	return facts
}

func (e *hookEngine) saveState(name string, state *hookState) {
	e.state[name] = hookMemory{
		ints:    maps.Clone(state.ints),
		strings: maps.Clone(state.strings),
	}
}

type hookState struct {
	facts        Facts
	sessionID    string
	runID        string
	input        string
	evaluationID uint64
	ints         map[string]int64
	strings      map[string]string
}

func (s *hookState) Facts() Facts {
	return s.facts
}

func (s *hookState) Int(name string) int64 {
	return s.ints[name]
}

func (s *hookState) SetInt(name string, value int64) {
	if s.ints == nil {
		s.ints = make(map[string]int64)
	}
	s.ints[name] = value
}

func (s *hookState) String(name string) string {
	return s.strings[name]
}

func (s *hookState) SetString(name, value string) {
	if s.strings == nil {
		s.strings = make(map[string]string)
	}
	s.strings[name] = value
}

// Tool arguments are JSON-shaped maps, slices and scalar values. Preserve their
// concrete types while isolating nested containers between callbacks.
func cloneFactValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneFactValue(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneFactValue(iter.Value()))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneFactValue(v.Index(i)))
		}
		return out
	default:
		return v
	}
}
