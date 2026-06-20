package runtime

import "nekocode/bot/hooks"

func (a *Agent) injectHint(h *hooks.Hint) {
	if h != nil {
		a.pendingHints = append(a.pendingHints, *h)
	}
}

func (a *Agent) applyTurnHints(hints []hooks.Hint) {
	if len(a.pendingHints) > 0 {
		hints = append(hints, a.pendingHints...)
		a.pendingHints = nil
	}
	a.ctxMgr.SetHints(hooks.FormatHints(hints))
}

func (a *Agent) synthesizeAndReturn(callback RunCallback) *RunResult {
	output := a.forceSynthesize()
	a.ctxMgr.AddAssistantResponse(output, "")
	if callback != nil {
		callback("chat", "", "", output)
	}
	return &RunResult{FinalOutput: output, Steps: a.step}
}

func (a *Agent) drainSteering() {
	for {
		select {
		case msg := <-a.steering:
			a.ctxMgr.Add("user", msg, "user")
		default:
			return
		}
	}
}
