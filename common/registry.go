// registry.go — 泛型 Registry，消除 tools/skill 等包中重复的锁+map+CRUD 模式。
package common

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a thread-safe named-item registry.
// T is the item type; nameFn extracts the string key from an item.
type Registry[T any] struct {
	mu      sync.RWMutex
	items   map[string]T
	nameFn  func(T) string
}

// NewRegistry creates a new generic registry.
// nameFn returns the key for a given item (e.g. func(t Tool) string { return t.Name() }).
func NewRegistry[T any](nameFn func(T) string) *Registry[T] {
	return &Registry[T]{
		items:  make(map[string]T),
		nameFn: nameFn,
	}
}

// Register adds or overwrites an item keyed by nameFn(item).
func (r *Registry[T]) Register(item T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[r.nameFn(item)] = item
}

// Unregister removes an item by name.
func (r *Registry[T]) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, name)
}

// Get returns the item and true if found, zero value and false otherwise.
func (r *Registry[T]) Get(name string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[name]
	return t, ok
}

// GetOrError returns the item or an error formatted with formatMsg.
// formatMsg receives the name as its argument (e.g. "tool %q not found").
func (r *Registry[T]) GetOrError(name string, formatMsg string) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[name]
	if !ok {
		var zero T
		return zero, fmt.Errorf(formatMsg, name)
	}
	return t, nil
}

// Has returns true if the item exists.
func (r *Registry[T]) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.items[name]
	return ok
}

// Len returns the number of registered items.
func (r *Registry[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}

// List returns all items sorted by name.
func (r *Registry[T]) List() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := r.sortedNames()
	out := make([]T, len(names))
	for i, n := range names {
		out[i] = r.items[n]
	}
	return out
}

// SortedNames returns all item names in sorted order.
func (r *Registry[T]) SortedNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sortedNames()
}

// Names returns all item names (unsorted).
func (r *Registry[T]) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.items))
	for n := range r.items {
		names = append(names, n)
	}
	return names
}

// Range iterates over all items in sorted name order.
// The callback receives the name and item. Return false to stop.
// The callback runs outside the lock — safe for I/O and nested registry calls.
func (r *Registry[T]) Range(fn func(name string, item T) bool) {
	r.mu.RLock()
	// Snapshot under lock, execute outside lock (like hooks.Evaluate).
	pairs := make([]struct {
		name string
		item T
	}, len(r.items))
	names := r.sortedNames()
	for i, n := range names {
		pairs[i] = struct {
			name string
			item T
		}{n, r.items[n]}
	}
	r.mu.RUnlock()

	for _, p := range pairs {
		if !fn(p.name, p.item) {
			return
		}
	}
}

// RegisterAll bulk-registers items, skipping names that already exist.
func (r *Registry[T]) RegisterAll(items []T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range items {
		name := r.nameFn(item)
		if _, exists := r.items[name]; !exists {
			r.items[name] = item
		}
	}
}

// sortedNames returns sorted names (caller must hold lock).
func (r *Registry[T]) sortedNames() []string {
	names := make([]string, 0, len(r.items))
	for n := range r.items {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
