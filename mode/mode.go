// Package mode defines interaction modes independently of UI and execution hosts.
package mode

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

type ID string
type Role string
type Strategy string
type ToolPolicy string

const (
	Standard          ID         = "standard"
	Plan              ID         = "plan"
	SuperAgent        ID         = "super-agent"
	Coordinator       Role       = "coordinator"
	Delegated         Strategy   = "delegated"
	CoordinationTools ToolPolicy = "coordination"
	General           Role       = "general"
	Planner           Role       = "planner"
	Direct            Strategy   = "direct"
	AllTools          ToolPolicy = "all"
	ReadOnlyTools     ToolPolicy = "read_only"
)

// Snapshot is a value captured for one run. It never follows later selections.
// Strategy selects an execution host; Role and ToolPolicy constrain its actor.
type Snapshot struct {
	ID         ID         `json:"id"`
	Version    int        `json:"version"`
	Role       Role       `json:"role"`
	Strategy   Strategy   `json:"strategy"`
	ToolPolicy ToolPolicy `json:"tool_policy"`
}

type Definition struct {
	Snapshot
	Label       string `json:"label"`
	Description string `json:"description"`
	Prompt      string `json:"-"`
}

// Registry is immutable after construction. Definitions are returned by value.
// Applications must bind each strategy to an implementation before exposing it.
type Registry struct{ definitions []Definition }

func NewRegistry(definitions ...Definition) (*Registry, error) {
	r := &Registry{}
	seen := map[ID]bool{}
	for _, d := range definitions {
		if d.ID == "" || strings.TrimSpace(string(d.ID)) != string(d.ID) || d.Version < 1 || d.Label == "" || d.Role == "" || d.Strategy == "" || d.ToolPolicy == "" {
			return nil, fmt.Errorf("invalid interaction mode %q", d.ID)
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("duplicate interaction mode %q", d.ID)
		}
		seen[d.ID] = true
		r.definitions = append(r.definitions, d)
	}
	return r, nil
}

func (r *Registry) Definitions() []Definition { return append([]Definition(nil), r.definitions...) }
func (r *Registry) Resolve(id ID) (Definition, error) {
	if id == "" {
		id = Standard
	}
	for _, d := range r.definitions {
		if d.ID == id {
			return d, nil
		}
	}
	return Definition{}, fmt.Errorf("unsupported interaction mode %q; select a supported mode", id)
}

//go:embed plan.md
var planPrompt string

//go:embed super_agent.md
var superAgentPrompt string

var builtin = mustRegistry(
	Definition{Snapshot: Snapshot{ID: Standard, Version: 1, Role: General, Strategy: Direct, ToolPolicy: AllTools}, Label: "Standard", Description: "Analyze and execute tasks under the current permissions"},
	Definition{Snapshot: Snapshot{ID: Plan, Version: 1, Role: Planner, Strategy: Direct, ToolPolicy: ReadOnlyTools}, Label: "Plan", Description: "Read-only exploration and planning; no shell, changes or delegation", Prompt: strings.TrimSpace(planPrompt)},
	Definition{Snapshot: Snapshot{ID: SuperAgent, Version: 1, Role: Coordinator, Strategy: Delegated, ToolPolicy: CoordinationTools}, Label: "Super", Description: "Coordinate tasks; delegate all execution to subagents", Prompt: strings.TrimSpace(superAgentPrompt)},
)

func mustRegistry(definitions ...Definition) *Registry {
	r, err := NewRegistry(definitions...)
	if err != nil {
		panic(err)
	}
	return r
}
func Definitions() []Definition         { return builtin.Definitions() }
func Resolve(id ID) (Definition, error) { return builtin.Resolve(id) }

// OneShot keeps /plan <task> compatible without changing the session selection.
func OneShot(input string) bool {
	fields := strings.Fields(input)
	return len(fields) > 1 && strings.EqualFold(fields[0], "/plan")
}

type snapshotKey struct{}

func WithSnapshot(ctx context.Context, s Snapshot) context.Context {
	return context.WithValue(ctx, snapshotKey{}, s)
}
func FromContext(ctx context.Context) (Snapshot, bool) {
	s, ok := ctx.Value(snapshotKey{}).(Snapshot)
	return s, ok
}

// Validate resolves a persisted/captured policy against this implementation.
// Unknown versions or policies cannot silently acquire Standard capabilities.
func Validate(s Snapshot) (Definition, error) {
	d, err := Resolve(s.ID)
	if err != nil {
		return Definition{}, err
	}
	if d.Snapshot != s {
		return Definition{}, fmt.Errorf("unsupported policy snapshot for interaction mode %q", s.ID)
	}
	return d, nil
}
