package http

import (
	"fmt"
	"net/textproto"
	"strings"
)

// hopByHopHeaders must not be set through configuration: the transport
// manages them, and smuggling values there breaks framing or proxying.
var hopByHopHeaders = map[string]bool{
	"host": true, "content-length": true, "connection": true,
	"transfer-encoding": true, "upgrade": true, "te": true,
	"trailer": true, "proxy-connection": true, "proxy-authorization": true,
	"proxy-authenticate": true,
}

// NormalizeHeaders validates and canonicalizes user-configured HTTP headers.
// Keys are canonicalized the same way net/http does so injection can compare
// them against request headers. Values are trimmed of surrounding spaces.
func NormalizeHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("header name must not be empty")
		}
		if !isValidHeaderName(key) {
			return nil, fmt.Errorf("header %q: invalid name", key)
		}
		canonical := textproto.CanonicalMIMEHeaderKey(key)
		if hopByHopHeaders[strings.ToLower(canonical)] {
			return nil, fmt.Errorf("header %q must not be set manually", canonical)
		}
		if strings.ContainsAny(canonical, "\r\n\x00") || strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("header %q contains newline or NUL", canonical)
		}
		normalized[canonical] = strings.TrimSpace(value)
	}
	return normalized, nil
}

// isValidHeaderName reports whether key only contains token characters
// allowed by RFC 9110. textproto.CanonicalMIMEHeaderKey canonicalizes any
// such name, so matching is case-insensitive on the wire.
func isValidHeaderName(key string) bool {
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}
