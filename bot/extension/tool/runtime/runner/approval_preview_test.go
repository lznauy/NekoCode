package runner

import (
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

func TestApprovalPreviewMatchesRememberedRules(t *testing.T) {
	for _, command := range []string{`echo hello && date`, `bash -c 'echo ok'`} {
		t.Run(command, func(t *testing.T) {
			e := newPermTestExecutor(t)
			root := t.TempDir()
			e.SetProjectStore(root)
			e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
			call := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": command}}
			decision := e.evaluatePermission(t.Context(), call, nil)
			var approval *protocol.ApprovalContext
			_, ok := e.promptConfirm(call, func(req protocol.ConfirmRequest) protocol.ConfirmReply {
				approval = req.Approval.Clone()
				return protocol.AllowRemembered()
			}, nil, decision)
			if !ok || approval == nil || approval.RememberWorkspace != root || len(approval.RememberRules) == 0 {
				t.Fatalf("missing preview: %+v", approval)
			}
			e.rememberAllowRule("shell", call.Args, decision.matchedRule)
			rules := e.permStore.RememberedRules(root)
			if len(rules) != len(approval.RememberRules) {
				t.Fatalf("persisted=%+v preview=%+v", rules, approval.RememberRules)
			}
			for i, rule := range rules {
				preview := approval.RememberRules[i]
				if preview.Tool != rule.Tool || preview.Literal != rule.Literal || rule.Literal == "" && preview.Specifier != rule.Specifier {
					t.Fatalf("preview differs from saved rule: %+v != %+v", preview, rule)
				}
			}
		})
	}
}

func TestApprovalExplainsExplicitAskSource(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell(make *)"}}, "/repo", "/home/user")
	d := e.evaluatePermission(t.Context(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "make test"}}, nil)
	if !d.prompt || d.approval == nil || d.approval.Source != "user_rule" || d.approval.Rule != "shell(make *)" {
		t.Fatalf("missing rule source: %+v", d.approval)
	}
}
