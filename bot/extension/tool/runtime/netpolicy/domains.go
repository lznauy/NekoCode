// Package netpolicy defines exact-domain egress authorization. A nil set means
// legacy unrestricted networking; a non-nil empty set means deny all egress.
package netpolicy

import (
	"fmt"
	"golang.org/x/net/idna"
	"net/netip"
	"slices"
	"strings"
)

func Normalize(domains []string) ([]string, error) {
	if domains == nil {
		return nil, nil
	}
	out := make([]string, 0, len(domains))
	for _, raw := range domains {
		name, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), "."))
		if err != nil || name == "" || len(name) > 253 || strings.ContainsAny(name, "/:*?@\\ \t\n\r") {
			return nil, fmt.Errorf("invalid outbound domain %q", raw)
		}
		if _, err := netip.ParseAddr(name); err == nil {
			return nil, fmt.Errorf("outbound domain must be a DNS name: %q", raw)
		}
		for _, label := range strings.Split(name, ".") {
			if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return nil, fmt.Errorf("invalid outbound domain %q", raw)
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return nil, fmt.Errorf("invalid outbound domain %q", raw)
				}
			}
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out, nil
}

func Parse(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		if v == nil {
			return nil, fmt.Errorf("outbound_domains must be an array")
		}
		return Normalize(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("outbound_domains entries must be strings")
			}
			out = append(out, s)
		}
		return Normalize(out)
	default:
		return nil, fmt.Errorf("outbound_domains must be an array")
	}
}

func Intersect(a, b []string) []string {
	if a == nil {
		return slices.Clone(b)
	}
	if b == nil {
		return slices.Clone(a)
	}
	out := []string{}
	for _, s := range a {
		if slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}

func Covers(have, need []string) bool {
	if have == nil {
		return true
	}
	if need == nil {
		return false
	}
	for _, s := range need {
		if !slices.Contains(have, s) {
			return false
		}
	}
	return true
}

func FromDetails(details map[string]any) ([]string, error) {
	raw, ok := details["outbound_domains"]
	if !ok {
		return nil, nil
	}
	return Parse(raw)
}
