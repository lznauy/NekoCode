package permission

import (
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
)

func grantAllowsRequest(g Grant, req core.PermissionRequest) bool {
	if !containsAllCapabilities(effectiveGrantCapabilities(g), req.Capabilities) {
		return false
	}
	if hasCapability(req.Capabilities, core.CapNetOutbound) {
		domains, err := netpolicy.FromDetails(req.Details)
		if err != nil {
			return false
		}
		have, err := netpolicy.Normalize(g.OutboundDomains)
		if err != nil || !netpolicy.Covers(have, domains) {
			return false
		}
	}
	if !hasCapability(req.Capabilities, core.CapFsWritePath) {
		return true
	}
	return ContainsAllWritePaths(normalizeWritePaths(g.WritePaths), WritePathsFromRequest(req))
}

func grantDeniesRequest(g Grant, req core.PermissionRequest) bool {
	if !intersectsCapability(effectiveGrantCapabilities(g), req.Capabilities) {
		return false
	}
	if !hasCapability(g.Capabilities, core.CapFsWritePath) || !hasCapability(req.Capabilities, core.CapFsWritePath) {
		return true
	}
	deniedPaths := normalizeWritePaths(g.WritePaths)
	if len(deniedPaths) == 0 {
		return true
	}
	return intersectsWritePath(deniedPaths, WritePathsFromRequest(req))
}

func containsAllCapabilities(have, need []string) bool {
	for _, n := range need {
		if !hasCapability(have, n) {
			return false
		}
	}
	return true
}

func intersectsCapability(left, right []string) bool {
	for _, l := range left {
		if hasCapability(right, l) {
			return true
		}
	}
	return false
}

func hasCapability(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// ContainsAllWritePaths reports whether every requested write path is covered
// by one of the authorized roots.
func ContainsAllWritePaths(have, need []string) bool {
	if len(need) == 0 {
		return true
	}
	if len(have) == 0 {
		return false
	}
	for _, n := range need {
		covered := false
		for _, h := range have {
			if pathWithinRoot(n, h) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func intersectsWritePath(left, right []string) bool {
	for _, l := range left {
		for _, r := range right {
			if pathWithinRoot(l, r) || pathWithinRoot(r, l) {
				return true
			}
		}
	}
	return false
}

func pathWithinRoot(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// WritePathsFromRequest extracts and normalizes writePaths from a permission
// request's Details map.
func WritePathsFromRequest(req core.PermissionRequest) []string {
	if req.Details == nil {
		return nil
	}
	switch v := req.Details["writePaths"].(type) {
	case []string:
		return normalizeWritePaths(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return normalizeWritePaths(out)
	case string:
		return normalizeWritePaths(strings.Split(v, ","))
	}
	return nil
}

func normalizeWritePaths(paths []string) []string {
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "~/") && home != "" {
			p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		p = filepath.Clean(p)
		if p == "." || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func effectiveGrantCapabilities(g Grant) []string {
	caps := slices.Clone(g.Capabilities)
	for i, capability := range caps {
		if capability == scopedNetworkCapability && g.OutboundDomains != nil {
			caps[i] = core.CapNetOutbound
		}
	}
	return caps
}
