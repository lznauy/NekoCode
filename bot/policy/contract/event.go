package contract

// NormalizeEvent gives every adapter identical defaults. It never mutates
// existing maps: a handler receives its own JSON/state snapshot at the boundary.
func NormalizeEvent(e Event) Event {
	if e.Version == 0 {
		e.Version = Version
	}
	if e.ActorID == "" {
		e.ActorID = "main"
	}
	if e.Args == nil {
		e.Args = map[string]any{}
	}
	if e.State.Ints == nil {
		e.State.Ints = map[string]int64{}
	}
	if e.State.Strings == nil {
		e.State.Strings = map[string]string{}
	}
	return e
}
