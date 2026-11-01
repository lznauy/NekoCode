package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"nekocode/protocol"
)

// Configuration describes registered behavior without executing callbacks or
// reading hook state. Owners must bump BehaviorVersion when behavior changes;
// undeclared callbacks explicitly make this diagnostic incomplete.
func (p *Policy) Configuration() protocol.PolicyConfiguration {
	type declaration struct {
		Name          string    `json:"name"`
		Point         HookPoint `json:"point"`
		Version       string    `json:"version"`
		Guard         bool      `json:"guard"`
		TimeoutNS     int64     `json:"timeout_ns"`
		SessionScoped bool      `json:"session_scoped"`
		Once          bool      `json:"once"`
	}
	declarations := []declaration{}
	out := protocol.PolicyConfiguration{Schema: 1}
	if p != nil {
		p.hooks.mu.Lock()
		out.Revision = p.hooks.nextVersion
		for _, hook := range p.hooks.hooks {
			declarations = append(declarations, declaration{hook.Name, hook.Point, hook.BehaviorVersion, hook.Guard, hookTimeout(hook.Timeout).Nanoseconds(), hook.SessionScoped, hook.Once})
			if hook.BehaviorVersion == "" {
				out.Incomplete = true
			}
		}
		p.hooks.mu.Unlock()
	}
	// Only fixed primitive fields above enter the hash, never callbacks, facts,
	// environment values, credential-bearing plugin config or hook-private state.
	data, _ := json.Marshal(struct {
		Schema int           `json:"schema"`
		Hooks  []declaration `json:"hooks"`
	}{1, declarations})
	digest := sha256.Sum256(data)
	out.Hash = hex.EncodeToString(digest[:])
	return out
}
