package tools

import "nekocode/llm"

// ToToolDefs converts tool descriptors to LLM tool definitions.
func ToToolDefs(descs []Descriptor) []llm.ToolDef {
	defs := make([]llm.ToolDef, len(descs))
	for i, d := range descs {
		props := make(map[string]llm.Property)
		var required []string
		for _, p := range d.Parameters {
			props[p.Name] = llm.Property{Type: p.Type, Description: p.Description}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		defs[i] = llm.ToolDef{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        d.Name,
				Description: d.Description,
				Parameters: llm.Parameters{
					Type: "object", Properties: props, Required: required,
				},
			},
		}
	}
	return defs
}
