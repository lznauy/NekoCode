package contract

import "fmt"

const (
	MaxStateEntries = 256
	MaxStateBytes   = 64 * 1024
)

// StateSize accounts for UTF-8 key/value bytes and eight bytes per int64.
// It deliberately avoids JSON encoding, escaping and temporary allocations.
func StateSize(s StatePatch) (entries, bytes int) {
	entries = len(s.Ints) + len(s.Strings)
	for k := range s.Ints {
		bytes += len(k) + 8
	}
	for k, v := range s.Strings {
		bytes += len(k) + len(v)
	}
	return entries, bytes
}

func ValidateState(s StatePatch) error {
	if entries := len(s.Ints) + len(s.Strings); entries > MaxStateEntries {
		return fmt.Errorf("hook state exceeds %d entries", MaxStateEntries)
	}
	_, bytes := StateSize(s)
	if bytes > MaxStateBytes {
		return fmt.Errorf("hook state exceeds %d bytes", MaxStateBytes)
	}
	return nil
}
