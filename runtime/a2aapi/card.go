package a2aapi

import "github.com/a2aproject/a2a-go/v2/a2a"

const defaultAgentVersion = "0.1.0"

func newAgentCard(endpoint, version string, secured bool) *a2a.AgentCard {
	if version == "" {
		version = defaultAgentVersion
	}
	card := &a2a.AgentCard{
		Name:        "NekoCode",
		Description: "An AI coding agent that can understand codebases, edit files, run commands, research information, and delegate work to subagents.",
		Version:     version,
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(endpoint, a2a.TransportProtocolHTTPJSON),
		},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []a2a.AgentSkill{
			{
				ID:          "software_engineering",
				Name:        "Software engineering",
				Description: "Understand, modify, test, and explain software projects in the configured workspace.",
				Tags:        []string{"coding", "debugging", "testing", "code-review"},
				Examples: []string{
					"Explain the architecture of this repository.",
					"Fix the failing tests and verify the result.",
				},
			},
			{
				ID:          "technical_research",
				Name:        "Technical research",
				Description: "Research technical topics and relate the findings to the current project.",
				Tags:        []string{"research", "documentation", "architecture"},
				Examples:    []string{"Compare the available implementation approaches for this feature."},
			},
		},
	}
	if secured {
		const scheme = a2a.SecuritySchemeName("bearer")
		card.SecuritySchemes = a2a.NamedSecuritySchemes{
			scheme: a2a.HTTPAuthSecurityScheme{
				Scheme:      "Bearer",
				Description: "Use the NekoCode daemon bearer token.",
			},
		}
		card.SecurityRequirements = a2a.SecurityRequirementsOptions{
			{scheme: a2a.SecuritySchemeScopes{}},
		}
	}
	return card
}
