// Package sse provides SSE (Server-Sent Events) parsing helpers.
package sse

import "strings"

// SSELineData extracts a data field's payload, removing at most one optional space.
// A field without a colon has an empty value, as specified by SSE.
func SSELineData(line string) (string, bool) {
	if line == "data" {
		return "", true
	}
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "), true
}
