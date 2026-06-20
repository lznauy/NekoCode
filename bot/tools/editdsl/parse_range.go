package editdsl

import (
	"fmt"
	"strconv"
	"strings"
)

// parseRange parses "N" or "N..M" into a 1-based inclusive range.
func parseRange(spec string) (start, end int, err error) {
	if idx := strings.Index(spec, ".."); idx >= 0 {
		start, err = strconv.Atoi(strings.TrimSpace(spec[:idx]))
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start line: %w", err)
		}
		end, err = strconv.Atoi(strings.TrimSpace(spec[idx+2:]))
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end line: %w", err)
		}
	} else {
		start, err = strconv.Atoi(strings.TrimSpace(spec))
		if err != nil {
			return 0, 0, fmt.Errorf("invalid line number: %w", err)
		}
		end = start
	}
	if start < 1 {
		return 0, 0, fmt.Errorf("line numbers must be >= 1, got %d", start)
	}
	if end < start {
		return 0, 0, fmt.Errorf("end line %d < start line %d", end, start)
	}
	return start, end, nil
}
