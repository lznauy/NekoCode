package agentprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

// ConfigurationHash describes declared execution settings. SystemPrompt is
// already covered by the actual request's system hash; Description is UI only.
// Model/tool availability is still observed from the actual provider request.
func (p Profile) ConfigurationHash() string {
	tools := slices.Clone(p.Tools)
	slices.Sort(tools)
	tools = slices.Compact(tools)
	if tools == nil {
		tools = []string{}
	}
	skills := p.Skills
	if skills == nil {
		skills = []string{}
	}
	steps := p.MaxSteps
	if steps == 0 {
		steps = DefaultMaxSteps
	}
	data, _ := json.Marshal(struct {
		RequiredRole string   `json:"required_role,omitempty"`
		Schema       int      `json:"schema"`
		Name         string   `json:"name"`
		Model        string   `json:"model"`
		Isolation    string   `json:"isolation"`
		Tools        []string `json:"tools"`
		Skills       []string `json:"skills"`
		MaxSteps     int      `json:"max_steps"`
	}{string(p.RequiredRole), 1, p.Name, p.Model, p.Isolation, tools, skills, steps})
	digest := sha256.Sum256(data)
	return "v1:" + hex.EncodeToString(digest[:])
}
