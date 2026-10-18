package runtime

import (
	"errors"
	"fmt"
)

// SessionSettings are transient, active-session preferences, not persisted
// process defaults. ApplySessionSettings owns their mutation boundary.
type SessionSettings struct {
	Model           string
	ReasoningEffort string
	FullAccess      bool
}

// ApplySessionSettings prevents runs and other runtime mutations from seeing
// a half-applied configuration. Rollback failures are included in the error.
func (r *Runtime) ApplySessionSettings(next SessionSettings) error {
	s := r.services
	supported := s.Models.ModelOptions != nil && s.Models.CurrentModel != nil &&
		s.Permissions.PermissionMode != nil && s.Models.SwitchSessionModel != nil &&
		s.Models.SetSessionReasoning != nil && s.Permissions.SetFullAccess != nil
	return r.mutation("apply_session_settings", supported, func() error {
		options, model := s.Models.ModelOptions()
		previous := SessionSettings{Model: model, ReasoningEffort: s.Models.CurrentModel().ReasoningEffort,
			FullAccess: s.Permissions.PermissionMode() == "full"}
		if next.Model == "" {
			next.Model = previous.Model
		}
		apply := func(settings SessionSettings) error {
			if settings.Model != "" {
				if _, err := s.Models.SwitchSessionModel(settings.Model); err != nil {
					return fmt.Errorf("switch model: %w", err)
				}
			}
			if err := s.Models.SetSessionReasoning(settings.ReasoningEffort); err != nil {
				return fmt.Errorf("set reasoning: %w", err)
			}
			s.Permissions.SetFullAccess(settings.FullAccess)
			return nil
		}
		if err := apply(next); err != nil {
			var rollback error
			// Reasoning settings may belong to individual model profiles.
			// Restore the target profile as well as the previously active one.
			if next.Model != previous.Model {
				for _, option := range options {
					if option.Name == next.Model {
						rollback = apply(SessionSettings{Model: next.Model, ReasoningEffort: option.ReasoningEffort, FullAccess: previous.FullAccess})
						break
					}
				}
			}
			rollback = errors.Join(rollback, apply(previous))
			if rollback != nil {
				return errors.Join(err, fmt.Errorf("rollback session settings: %w", rollback))
			}
			return err
		}
		return nil
	})
}
