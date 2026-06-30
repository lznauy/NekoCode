package runtime

import "nekocode/bot/hooks"

func (a *Agent) injectHint(h *hooks.Hint) {
	if h != nil {
		a.run.pendingHints = append(a.run.pendingHints, *h)
	}
}

func (a *Agent) applyTurnHints(hints []hooks.Hint) {
	if len(a.run.pendingHints) > 0 {
		hints = append(hints, a.run.pendingHints...)
		a.run.pendingHints = nil
	}
	a.deps.ctxMgr.SetHints(hooks.FormatHints(hints))
}

func (a *Agent) drainSteering() {
	for {
		select {
		case msg := <-a.life.steering:
			a.deps.ctxMgr.Add("user", msg, "user")
		default:
			return
		}
	}
}
