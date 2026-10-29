package shell

import (
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"strings"
)

func sandboxRequestFromArgs(args map[string]any) sandboxRequest {
	r := sandboxRequest{
		Mode:          optStringArg(args, "sandbox_mode"),
		Network:       optBoolArg(args, "network"),
		WritableRoots: optStringSliceArg(args, "writable_roots"),
	}
	if raw, ok := args["outbound_domains"]; ok {
		r.OutboundDomains, r.domainErr = netpolicy.Parse(raw)
	}
	return r
}

// optStringSliceArg extracts an optional []string from args[key]. Accepts
// []any, []string, or nil/absent (returns nil).
func optStringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func optStringArg(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	s, _ := raw.(string)
	return strings.TrimSpace(s)
}

func optBoolArg(args map[string]any, key string) bool {
	raw, ok := args[key]
	if !ok || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return false
}
