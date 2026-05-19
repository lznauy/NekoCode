package subagent

type AgentType struct {
	Name               string
	SystemPrompt       string
	Tools              []string
	OmitProjectContext bool
	ReadOnly           bool
}

type RunConfig struct {
	Prompt          string
	AgentType       AgentType
	Cwd             string
	ProjectContext  string
	Thoroughness    string
	TokenBudget     int // parent agent's token budget
	OnPhase         func(phase string)
	AddTokens       func(prompt, compl int)
	DisableThinking bool
}

var builtins = map[string]AgentType{}

func register(a AgentType) { builtins[a.Name] = a }

func Get(name string) (AgentType, bool) {
	a, ok := builtins[name]
	return a, ok
}

func List() []AgentType {
	out := make([]AgentType, 0, len(builtins))
	for _, a := range builtins {
		out = append(out, a)
	}
	return out
}
