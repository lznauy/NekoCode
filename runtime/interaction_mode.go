package runtime

import "nekocode/mode"

// InteractionMode is a read model; it never aliases permission Mode fields.
type InteractionMode struct {
	Selected mode.ID           `json:"selected"`
	Options  []mode.Definition `json:"options"`
}

func (r *Runtime) InteractionMode() InteractionMode {
	s := r.services.Modes
	if s.Current == nil {
		return InteractionMode{Selected: mode.Standard}
	}
	return InteractionMode{Selected: s.Current(), Options: s.Options()}
}

func (r *Runtime) SetInteractionMode(id mode.ID) error {
	return r.mutation("set_interaction_mode", r.services.Modes.Set != nil, func() error {
		if err := r.services.Modes.Set(id); err != nil {
			return err
		}
		r.publishInteractionModeChanged()
		return nil
	})
}

func (r *Runtime) publishInteractionModeChanged() {
	r.events.Publish(Event{Type: EventInteractionModeChanged, Source: SourceRef{Kind: "runtime"}, Payload: r.services.Modes.Current()})
}
