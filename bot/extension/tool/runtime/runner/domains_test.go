package runner

import (
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"testing"
)

func TestDomainPreapprovalAndMerge(t *testing.T) {
	predicted := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}, Details: map[string]any{"outbound_domains": []string{"example.com"}}}
	a := escalationApproval{predicted: &predicted}
	if !a.requestCoveredBy(predicted) || a.requestCoveredBy(core.PermissionRequest{Capabilities: predicted.Capabilities}) || a.requestCoveredBy(core.PermissionRequest{Capabilities: predicted.Capabilities, Details: map[string]any{"outbound_domains": []string{"evil.com"}}}) {
		t.Fatal("domain preapproval widened")
	}
	merged := mergeSandboxArgs(map[string]any{"network": true, "outbound_domains": []string{"evil.com"}}, permission.SandboxProfile{OutboundDomains: []string{"example.com"}})
	domains, ok := merged["outbound_domains"].([]string)
	if !ok || domains == nil || len(domains) != 0 {
		t.Fatalf("merge lost empty intersection: %v", merged)
	}
	approval := approvalContextFromPermission(predicted)
	if len(approval.OutboundDomains) != 1 || approval.Clone().OutboundDomains[0] != "example.com" {
		t.Fatal("approval lost domain scope")
	}
}
