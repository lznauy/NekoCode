package http

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// NormalizeSecureURL trims configuration whitespace and validates the URL.
// An empty value is allowed so optional service endpoints can share this path.
func NormalizeSecureURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if err := ValidateSecureURL(raw); err != nil {
		return "", err
	}
	return raw, nil
}

// ValidateSecureURL accepts HTTPS, or cleartext HTTP on literal loopback IPs.
// Userinfo and fragments are rejected to avoid ambiguous credential handling.
func ValidateSecureURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("invalid service URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return fmt.Errorf("service URL requires HTTPS (HTTP is allowed on loopback IPs only)")
	}
	return nil
}
