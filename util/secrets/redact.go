// Package secrets prevents loaded application credentials from being echoed by
// tool output. It is not a general secret detector or filesystem isolation.
package secrets

import (
	"bytes"
	"encoding/json"
	"sort"
	"sync"
)

const Replacement = "[REDACTED_CREDENTIAL]"

var registry struct {
	sync.RWMutex
	values  map[string]struct{}
	version uint64
}

// Register retains known credential values only in memory for output filtering.
func Register(values ...string) {
	registry.Lock()
	defer registry.Unlock()
	if registry.values == nil {
		registry.values = map[string]struct{}{}
	}
	for _, v := range values {
		if v == "" {
			continue
		}
		// JSON file readers may echo the escaped spelling rather than its value.
		b, _ := json.Marshal(v)
		for _, pattern := range []string{v, string(b[1 : len(b)-1])} {
			if _, exists := registry.values[pattern]; !exists {
				registry.values[pattern] = struct{}{}
				registry.version++
			}
		}
	}
}

type Filter struct {
	buckets [256][]string
	pending []byte
	version uint64
}

// NewFilter follows loaded credentials for one output stream. A filter must
// not be shared by concurrent streams. Keep trailing partial matches private
// until another chunk arrives or Finish is called with final=true.
func NewFilter() *Filter {
	f := &Filter{}
	f.refresh()
	return f
}

func (f *Filter) Transform(p []byte, final bool) []byte {
	f.refresh()
	data := append(f.pending, p...)
	f.pending = nil
	out := make([]byte, 0, len(data))
	marker := []byte(Replacement)
	for i := 0; i < len(data); {
		remaining := data[i:]
		protected := bytes.HasPrefix(remaining, marker)
		if !final && len(remaining) < len(marker) && bytes.HasPrefix(marker, remaining) {
			f.pending = append([]byte(nil), remaining...)
			break
		}
		held, matched := false, false
		for _, s := range f.buckets[data[i]] {
			if protected && len(s) <= len(marker) {
				continue
			}
			if len(remaining) < len(s) && !final && bytes.HasPrefix([]byte(s), remaining) {
				f.pending = append([]byte(nil), remaining...)
				held = true
				break
			}
			if bytes.HasPrefix(remaining, []byte(s)) {
				out = append(out, Replacement...)
				i += len(s)
				matched = true
				break
			}
		}
		if held {
			break
		}
		if !matched && protected {
			out = append(out, marker...)
			i += len(marker)
			continue
		}
		if !matched {
			out = append(out, data[i])
			i++
		}
	}
	return out
}

func Redact(text string) string { return string(NewFilter().Transform([]byte(text), true)) }

func (f *Filter) refresh() {
	registry.RLock()
	defer registry.RUnlock()
	if f.version == registry.version {
		return
	}
	f.buckets = [256][]string{}
	for s := range registry.values {
		f.buckets[s[0]] = append(f.buckets[s[0]], s)
	}
	for i := range f.buckets {
		sort.Slice(f.buckets[i], func(a, b int) bool { return len(f.buckets[i][a]) > len(f.buckets[i][b]) })
	}
	f.version = registry.version
}
