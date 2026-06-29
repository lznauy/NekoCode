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
	out := &Result{}
	if r.Hint != nil {
		out.Hint = &Hint{
			Type:     r.Hint.Type,
			Severity: r.Hint.Severity,
			Content:  r.Hint.Content,
		}
	}
	if r.Stop != nil {
		sr := StopReason(r.Stop.Reason)
		out.Stop = &sr
	}
	if r.BlockTool != nil {
		out.BlockTool = &BlockTool{
			Tool:   r.BlockTool.Tool,
			Reason: r.BlockTool.Reason,
		}
	}
	if r.RequireTool != nil {
		out.RequireTool = &RequireTool{
			Tool:   r.RequireTool.Tool,
			Reason: r.RequireTool.Reason,
		}
	}
	if r.BlockFinal != nil {
		out.BlockFinal = &BlockFinal{
			Reason: r.BlockFinal.Reason,
		}
	}
	if r.StatePatch != nil {
		out.StatePatch = &StatePatch{
			Ints:    r.StatePatch.Ints,
			Strings: r.StatePatch.Strings,
		}
	}
	return out
}
