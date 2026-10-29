package shell

import (
	"errors"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
	"testing"
)

func TestShellDomainPermissionAndValidation(t *testing.T) {
	tool := &ShellTool{}
	ctx := workspace.WithManager(t.Context(), workspace.New(t.TempDir(), nil))
	args := map[string]any{"command": "true", "outbound_domains": []any{"EXAMPLE.COM."}}
	_, err := tool.Execute(ctx, args)
	var required core.RequiredPermissionError
	if !errors.As(err, &required) {
		t.Fatal(err)
	}
	domains, ok := required.Request.Details["outbound_domains"].([]string)
	if !ok || len(domains) != 1 || domains[0] != "example.com" {
		t.Fatalf("request: %+v", required.Request)
	}
	for _, raw := range []any{nil, "example.com", []any{1}, []string{"*.example.com"}} {
		args["outbound_domains"] = raw
		_, err := tool.Execute(ctx, args)
		if err == nil || errors.As(err, &required) {
			t.Fatalf("bad domains accepted: %v %v", raw, err)
		}
	}
	args["outbound_domains"] = []string{}
	args["sandbox_mode"] = "host"
	if _, err := tool.Execute(ctx, args); err == nil {
		t.Fatal("host restriction dropped")
	}
}
func TestShellDomainDeclarationIsCeiling(t *testing.T) {
	engine, err := permission.NewEngineForWorkspace(permission.PermissionsDecl{Sandbox: map[string]permission.SandboxProfile{"shell": {OutboundDomains: []string{"example.com"}}}}, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tool := &ShellTool{permEngine: engine}
	request := tool.effectiveSandboxRequest(map[string]any{"command": "true", "network": true, "outbound_domains": []string{"other.com"}})
	if request.OutboundDomains == nil || len(request.OutboundDomains) != 0 || request.Network {
		t.Fatalf("declaration widened: %+v", request)
	}
}
