package shell

import (
	"fmt"
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"strings"
)

func sandboxRequestFromArgs(args map[string]any) sandboxRequest {
	r := sandboxRequest{}
	if raw, supplied := args["sandbox_mode"]; supplied {
		mode, ok := raw.(string)
		if !ok {
			r.validationErr = fmt.Errorf("sandbox_mode must be read-only, workspace-write, or host")
			return r
		}
		r.Mode = strings.TrimSpace(mode)
		if r.Mode != "read-only" && r.Mode != "workspace-write" && r.Mode != "host" {
			r.validationErr = fmt.Errorf("sandbox_mode must be read-only, workspace-write, or host; omit it for workspace-write")
			return r
		}
	}
	if raw, supplied := args["name"]; supplied {
		if _, ok := raw.(string); !ok {
			r.validationErr = fmt.Errorf("name must be a string; omit it for an automatically assigned process name")
			return r
		}
	}
	if raw, supplied := args["network"]; supplied {
		var ok bool
		r.Network, ok = raw.(bool)
		if !ok {
			r.validationErr = fmt.Errorf("network must be a boolean (true or false)")
			return r
		}
	}
	r.WritableRoots, r.validationErr = optStringSliceArg(args, "writable_roots")
	if r.validationErr != nil {
		return r
	}
	if raw, supplied := args["outbound_domains"]; supplied {
		r.OutboundDomains, r.validationErr = netpolicy.Parse(raw)
	}
	return r
}

func optStringSliceArg(args map[string]any, key string) ([]string, error) {
	raw, supplied := args[key]
	if !supplied {
		return nil, nil
	}
	var items []any
	switch values := raw.(type) {
	case []string:
		for _, value := range values {
			items = append(items, value)
		}
	case []any:
		items = values
	default:
		return nil, fmt.Errorf("%s must be an array of nonempty directory paths", key)
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s[%d] must be a nonempty directory path; no command was started", key, i)
		}
		out = append(out, strings.TrimSpace(value))
	}
	return out, nil
}

func optStringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}
