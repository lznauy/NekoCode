package policy

import (
	"context"
	"maps"
	"nekocode/bot/policy/contract"
	"strings"

	hookplugin "nekocode/bot/policy/plugin"
)

func LoadPluginHooks(pluginRoot, hooksPath string) ([]Hook, error) {
	loaded, err := hookplugin.Load(pluginRoot, hooksPath)
	if err != nil {
		return nil, err
	}
	hooks := make([]Hook, 0, len(loaded))
	for _, h := range loaded {
		h.Name = "plugin:" + pluginRoot + ":" + strings.TrimPrefix(h.Name, "plugin:")
		hooks = append(hooks, adaptPluginHook(h))
	}
	return hooks, nil
}

func adaptPluginHook(h hookplugin.Hook) Hook {
	return Hook{Name: h.Name, Point: HookPoint(h.Point), Guard: h.Guard, Timeout: h.Timeout, Once: h.Once, SessionScoped: h.SessionScoped,
		Run: func(ctx context.Context, s State) (*Result, error) {
			facts := s.Facts()
			event := hookplugin.Event{HookID: h.Name, Context: ctx, Version: contract.Version, Point: string(h.Point), Tool: facts.Tool.Name, Error: facts.Tool.Error, Args: facts.Tool.Args, Output: facts.Tool.Output, ErrorMessage: facts.Tool.ErrorMessage, Batch: facts.Batch, Intent: string(facts.Response.Intent), Text: facts.Response.Text, State: pluginStateSnapshot(s), ActorID: "main"}
			if facts.ActorID != "" {
				event.ActorID = facts.ActorID
			}
			if state, ok := s.(*hookState); ok {
				event.SessionID = state.sessionID
				event.RunID = state.runID
				event.Input = state.input
				event.EvaluationID = state.evaluationID
			}
			result := h.On(event)
			if result != nil && result.Err != nil {
				return nil, result.Err
			}
			return result, nil
		}}
}

func applyStatePatch(state State, patch *contract.StatePatch) {
	for key, value := range patch.Ints {
		state.SetInt(key, value)
	}
	for key, value := range patch.Strings {
		state.SetString(key, value)
	}
}

func pluginStateSnapshot(s State) hookplugin.StatePatchResult {
	if state, ok := s.(*hookState); ok {
		return hookplugin.StatePatchResult{Ints: maps.Clone(state.ints), Strings: maps.Clone(state.strings)}
	}
	return hookplugin.StatePatchResult{}
}
