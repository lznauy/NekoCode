package llm

func NewClient(provider, apiKey, baseURL, model string, thinkingBudget int) LLM {
	return newClient(provider, apiKey, baseURL, model, thinkingBudget)
}

func Clone(provider, apiKey, baseURL, model string, thinkingBudget int) LLM {
	return newClient(provider, apiKey, baseURL, model, thinkingBudget)
}

func newClient(provider, apiKey, baseURL, model string, thinkingBudget int) LLM {
	switch provider {
	case "anthropic":
		c := NewAnthropic(apiKey, baseURL, model)
		c.SetThinkingBudget(thinkingBudget)
		return c
	case "deepseek":
		c := NewDeepSeek(apiKey, baseURL, model)
		if thinkingBudget < 0 {
			c.SetDisableThinking(true)
		} else if thinkingBudget > 0 {
			c.SetThinkingBudget(thinkingBudget)
		}
		return c
	default:
		return nil
	}
}
