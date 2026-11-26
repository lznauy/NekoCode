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
	return NormalizeServiceURL(raw, false)
}

// NormalizeServiceURL permits an empty optional endpoint.
func NormalizeServiceURL(raw string, allowInsecureHTTP bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if err := ValidateServiceURL(raw, allowInsecureHTTP); err != nil {
		return "", err
	}
	return raw, nil
}

// ValidateSecureURL accepts HTTPS, or cleartext HTTP on literal loopback IPs.
// Userinfo and fragments are rejected to avoid ambiguous credential handling.
func ValidateSecureURL(raw string) error {
	return ValidateServiceURL(raw, false)
}

// ValidateServiceURL optionally permits cleartext HTTP for a configured service.
func ValidateServiceURL(raw string, allowInsecureHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("invalid service URL")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if allowInsecureHTTP || net.ParseIP(u.Hostname()).IsLoopback() {
			return nil
		}
		return fmt.Errorf("service URL requires HTTPS (HTTP is allowed on loopback IPs only)")
	default:
		return fmt.Errorf("service URL requires HTTP or HTTPS")
	}
}
