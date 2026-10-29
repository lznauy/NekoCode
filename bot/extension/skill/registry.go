package skill

import (
	"fmt"
	"maps"
	"strings"
	"sync"

	utilregistry "nekocode/util/registry"
)

// registry manages loaded skills, thread-safe.
type registry struct {
	*utilregistry.Registry[*Skill]
	loaded   sync.Map
	errorsMu sync.RWMutex
	errors   map[string]string
}

func newRegistry() *registry {
	return &registry{
		errors:   make(map[string]string),
		Registry: utilregistry.New[*Skill](func(s *Skill) string { return s.Name }),
	}
}

// Load discovers and registers skills under dirs. Skills that fail to parse
// are skipped; already-registered names win.
func (r *registry) Load(dirs []string) {
	for _, p := range discoverSkills(dirs) {
		sk, err := loadSkill(p)
		if err != nil {
			r.errorsMu.Lock()
			r.errors[p] = err.Error()
			r.errorsMu.Unlock()
			continue
		}
		if !r.Has(sk.Name) {
			r.Register(sk)
		}
	}
}

func (r *registry) MarkLoaded(name string) {
	r.loaded.Store(name, true)
}

func (r *registry) ClearLoaded() {
	r.loaded.Clear()
}

func (r *registry) IsLoaded(name string) bool {
	_, ok := r.loaded.Load(name)
	return ok
}

func (r *registry) LoadedSet() map[string]bool {
	out := make(map[string]bool)
	r.loaded.Range(func(key, value any) bool {
		out[key.(string)] = true
		return true
	})
	return out
}

func (r *registry) namesString() string {
	names := r.ModelNames()
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func (r *registry) ModelNames() []string {
	names := make([]string, 0)
	for _, sk := range r.List() {
		if !sk.DisableModelInvocation {
			names = append(names, sk.Name)
		}
	}
	return names
}

func (r *registry) ForModel(name string) (*Skill, error) {
	sk, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("skill not found: %s (available: %s)", name, r.namesString())
	}
	if sk.DisableModelInvocation {
		return nil, fmt.Errorf("skill %q is manual-only; the user must select it explicitly", name)
	}
	return sk, nil
}

func (r *registry) LoadErrors() map[string]string {
	r.errorsMu.RLock()
	defer r.errorsMu.RUnlock()
	return maps.Clone(r.errors)
}
