package hooks

import hookplugin "nekocode/bot/hooks/plugin"

func LoadPluginHooks(pluginRoot, hooksPath string) ([]Hook, error) {
	loaded, err := hookplugin.Load(pluginRoot, hooksPath)
	if err != nil {
		return nil, err
	}
	hooks := make([]Hook, 0, len(loaded))
	for _, h := range loaded {
		hooks = append(hooks, adaptPluginHook(h))
	}
	return hooks, nil
}

func adaptPluginHook(h hookplugin.Hook) Hook {
	return Hook{
		Name:  h.Name,
		Point: HookPoint(h.Point),
		On: func(s *Snapshot) *Result {
			if h.Once {
				if s.flag(StoreSessionStarted) {
					return nil
				}
				s.set(StoreSessionStarted, 1)
			}
			return adaptPluginResult(h.On(hookplugin.Event{
				Tool:  s.Tool,
				Error: s.Error,
			}))
		},
	}
}

func adaptPluginResult(r *hookplugin.Result) *Result {
	if r == nil {
		return nil
	}
	if r.Hint == nil {
		return &Result{}
	}
	return &Result{Hint: &Hint{
		Type:     r.Hint.Type,
		Severity: r.Hint.Severity,
		Content:  r.Hint.Content,
	}}
}
