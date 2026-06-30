package runtime

func (a *Agent) AddTokens(prompt, completion int) {
	a.tokens.add(prompt, completion)
}

func (a *Agent) TokenUsage() (prompt, completion int) {
	return a.tokens.total(a.ContextTokens())
}

func (a *Agent) TurnTokenUsage() (prompt, completion int) {
	return a.tokens.turn(a.ContextTokens())
}

func (a *Agent) ContextTokens() int {
	_, tokens, _ := a.deps.ctxMgr.Stats()
	return tokens
}
