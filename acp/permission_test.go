package acp

import (
	"strings"
	"testing"

	controlruntime "nekocode/runtime"
)

func TestPermissionMapping(t *testing.T) {
	summary := approvalSummary(controlruntime.ApprovalView{Approval: &controlruntime.ApprovalContext{
		Risk: "high", Capabilities: []string{"network"}, WritePaths: []string{"/workspace"},
		OutboundDomains: []string{"example.com"},
	}})
	for _, want := range []string{"Risk: high", "Capabilities: network", "Write paths: /workspace", "Outbound domains: example.com"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("approval summary = %q; want %q", summary, want)
		}
	}
	var forged permissionResponse
	forged.Outcome.Outcome = "selected"
	forged.Outcome.OptionID = "allow_always"
	if decision := permissionDecision(forged, true, false); decision.Allowed || decision.Remember {
		t.Fatalf("unadvertised persistent decision accepted: %#v", decision)
	}
}

func TestApprovalSummaryOutboundDomains(t *testing.T) {
	for _, tc := range []struct {
		name    string
		domains []string
		want    string
	}{
		{name: "unspecified legacy network", domains: nil},
		{name: "all outbound denied", domains: []string{}, want: "Outbound domains: none (all outbound network access denied)"},
		{name: "domain allowlist", domains: []string{"example.com", "api.example.com"}, want: "Outbound domains: example.com, api.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary := approvalSummary(controlruntime.ApprovalView{Approval: &controlruntime.ApprovalContext{
				Reason: "network policy", OutboundDomains: tc.domains,
			}})
			if !strings.Contains(summary, "Reason: network policy") {
				t.Fatalf("approval summary lost existing fields: %q", summary)
			}
			if tc.want == "" {
				if strings.Contains(summary, "Outbound domains:") {
					t.Fatalf("unspecified domains must not look like an explicit policy: %q", summary)
				}
				return
			}
			if !strings.Contains(summary, tc.want) {
				t.Fatalf("approval summary = %q; want %q", summary, tc.want)
			}
		})
	}
}

func TestPermissionDecisionDistinguishesRPCFailure(t *testing.T) {
	for _, tc := range []struct {
		name, outcome, option        string
		valid, allowAlways           bool
		allowed, remember, cancelled bool
	}{
		{name: "failed RPC", cancelled: true},
		{name: "failed RPC with stale allow", outcome: "selected", option: "allow_once", cancelled: true},
		{name: "explicit rejection", outcome: "selected", option: "reject_once", valid: true},
		{name: "dismissed dialog", outcome: "cancelled", valid: true},
		{name: "allow once", outcome: "selected", option: "allow_once", valid: true, allowed: true},
		{name: "allow project", outcome: "selected", option: "allow_always", valid: true, allowAlways: true, allowed: true, remember: true},
		{name: "unadvertised project allow", outcome: "selected", option: "allow_always", valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var response permissionResponse
			response.Outcome.Outcome = tc.outcome
			response.Outcome.OptionID = tc.option
			decision := permissionDecision(response, tc.valid, tc.allowAlways)
			if decision.Allowed != tc.allowed || decision.Remember != tc.remember || decision.Cancelled != tc.cancelled {
				t.Fatalf("decision=%+v; want allowed=%v remember=%v cancelled=%v", decision, tc.allowed, tc.remember, tc.cancelled)
			}
			if reply := decision.ConfirmReply(); reply.Cancelled != tc.cancelled {
				t.Fatalf("lost cancellation provenance: %+v", reply)
			}
		})
	}
}
