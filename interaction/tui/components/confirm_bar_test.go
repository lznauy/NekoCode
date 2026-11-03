package components

import (
	"strings"
	"testing"

	"nekocode/interaction/tui/styles"
	"nekocode/protocol"
	controlruntime "nekocode/runtime"
)

func TestPermissionConfirmDoesNotRepeatCommand(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args: map[string]any{
			"command": `echo "喵~ bash 命令测试成功！当前工作目录: $(pwd)" && date`,
		},
		Kind: controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{
			Reason:       "command requests unsandboxed host execution",
			Capabilities: []string{"process.host"},
			Scope:        controlruntime.ApprovalScopeOnce,
			Workspace:    "/repo",
		},
	}, nil)

	view := cb.View(100, 40)
	if strings.Contains(view, "echo") || strings.Contains(view, "pwd") {
		t.Fatalf("permission confirm should not repeat the full command:\n%s", view)
	}
	for _, want := range []string{"主机执行", "仅本次", "/repo"} {
		if !strings.Contains(view, want) {
			t.Fatalf("permission confirm missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Command class") || strings.Contains(view, "Capabilities:") {
		t.Fatalf("permission confirm should not expose raw backend labels:\n%s", view)
	}
}

func TestWorkspacePermissionConfirmShowsPath(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "workspace",
		Args: map[string]any{
			"path":           "/repo/other",
			"access":         "read-only",
			"requested_path": "/repo/other/src/a.go",
		},
		Kind: controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{
			Reason:    "add read-only workspace for read",
			Scope:     controlruntime.ApprovalScopeProject,
			Workspace: "/repo/other",
		},
	}, nil)

	view := cb.View(100, 40)
	for _, want := range []string{"/repo/other", "/repo/other/src/a.go"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace confirm missing %q:\n%s", want, view)
		}
	}
}

func TestUnifiedPermissionConfirmAllowsOnce(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	ch := make(chan controlruntime.ConfirmReply, 1)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args:     map[string]any{"command": "go get example.com/pkg"},
		Kind:     controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{Scope: controlruntime.ApprovalScopeOnce},
	}, func(ok, remember bool) {
		ch <- controlruntime.ConfirmReply{Allowed: ok, Remember: ok && remember}
	})

	view := cb.View(100, 40)
	if strings.Contains(view, "并授权") {
		t.Fatalf("unified confirm must not expose a separate escalation action:\n%s", view)
	}
	if !strings.Contains(view, "仅本次允许") {
		t.Fatalf("confirm view should expose plain one-time approval:\n%s", view)
	}

	cb.Submit()
	reply := <-ch
	if !reply.Allowed || reply.Remember {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestUnifiedPermissionConfirmCanRemember(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	ch := make(chan controlruntime.ConfirmReply, 1)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args: map[string]any{
			"command": "go get example.com/pkg",
		},
		Kind:     controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{Scope: controlruntime.ApprovalScopeProject},
	}, func(ok, remember bool) {
		ch <- controlruntime.ConfirmReply{Allowed: ok, Remember: ok && remember}
	})

	view := cb.View(100, 40)
	if strings.Contains(view, "并授权") {
		t.Fatalf("confirm view must not expose merged allow+escalate button:\n%s", view)
	}
	if !strings.Contains(view, "始终允许") {
		t.Fatalf("confirm view should expose remember option for project scope:\n%s", view)
	}

	// Option order: 0=仅本次允许, 1=始终允许, 2=拒绝.
	cb.Move(1)
	cb.Submit()
	reply := <-ch
	if !reply.Allowed || !reply.Remember {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestDestructiveConfirmDefaultsToCancel(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	confirmed := true
	cb.SetDestructive("删除会话", "确定删除 session_1？", "删除", func(ok bool) {
		confirmed = ok
	})

	view := cb.View(80, 24)
	for _, want := range []string{"删除会话", "session_1", "删除", "取消"} {
		if !strings.Contains(view, want) {
			t.Fatalf("destructive confirm missing %q:\n%s", want, view)
		}
	}
	// Options render horizontally with cancel first (the safe default).
	if cb.Selected() != 0 {
		t.Fatalf("selected = %d, want cancel option", cb.Selected())
	}
	cb.Submit()
	if confirmed {
		t.Fatal("default destructive action should cancel")
	}

	// Moving right highlights the destructive accept action.
	confirmed = false
	cb.SetDestructive("删除会话", "确定删除 session_1？", "删除", func(ok bool) {
		confirmed = ok
	})
	cb.Move(1)
	cb.Submit()
	if !confirmed {
		t.Fatal("move then submit should confirm deletion")
	}
}

func TestCombinedApprovalShowsCommandRiskAndCapabilities(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args: map[string]any{
			"command": `bash -c "$(cat task.txt)"`,
		},
		Kind: controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{
			Risk:            "dynamic shell execution",
			Reason:          "command requests sandbox profile: net.outbound",
			Capabilities:    []string{"net.outbound"},
			Scope:           controlruntime.ApprovalScopeProject,
			Structures:      []string{"command_substitution", "shell_command_string"},
			WritePaths:      []string{"/tmp/cache"},
			OutboundDomains: []string{"registry.npmjs.org"},
			Combined:        true,
		},
	}, nil)

	view := cb.View(100, 40)
	for _, want := range []string{"执行确认", `bash -c`, "原因: 运行时生成内容", "出站网络", "/tmp/cache", "registry.npmjs.org", "始终允许"} {
		if !strings.Contains(view, want) {
			t.Fatalf("combined approval missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "并授权") {
		t.Fatalf("combined approval exposed a redundant action:\n%s", view)
	}
}

func TestDynamicApprovalWithoutCapabilitiesShowsExecutionRisk(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args:     map[string]any{"command": `env -S "bash -c echo"`},
		Kind:     controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{
			Risk:       "dynamic shell execution",
			Reason:     "dynamic shell execution",
			Scope:      controlruntime.ApprovalScopeProject,
			Structures: []string{"dynamic_command"},
		},
	}, nil)

	view := cb.View(100, 40)
	for _, want := range []string{"执行确认", "原因: 运行时生成内容"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dynamic approval missing %q:\n%s", want, view)
		}
	}
	for _, jargon := range []string{"动态执行结构", "动态命令名", "执行结构", "每次都需要确认"} {
		if strings.Contains(view, jargon) {
			t.Fatalf("dynamic approval still reads as jargon (%q):\n%s", jargon, view)
		}
	}
	if strings.Contains(view, "开放所列权限") {
		t.Fatalf("dynamic-only approval invented capabilities:\n%s", view)
	}
}

func TestJevJudgedApprovalShowsFooterNote(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args:     map[string]any{"command": "rm -rf build"},
		Kind:     controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{
			Risk:   "jev: judged dangerous",
			Reason: "jev: judged dangerous",
			Scope:  controlruntime.ApprovalScopeProject,
		},
	}, nil)

	view := cb.View(100, 40)
	for _, want := range []string{"原因: 风险判定不安全"} {
		if !strings.Contains(view, want) {
			t.Fatalf("jev approval missing %q:\n%s", want, view)
		}
	}
}

func TestNonJevApprovalHasNoJevFooter(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args:     map[string]any{"command": "rm -rf build"},
		Kind:     controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{
			Risk:   "dynamic shell execution",
			Reason: "dynamic shell execution",
			Scope:  controlruntime.ApprovalScopeProject,
		},
	}, nil)

	if view := cb.View(100, 40); strings.Contains(view, "经过 Jev 判定") {
		t.Fatalf("non-jev approval must not show the jev footer:\n%s", view)
	}
}

func TestPartialShellApprovalShowsOnlyNewGrants(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{ToolName: "shell", Args: map[string]any{"command": "curl https://a.test && python report.py"}, Kind: controlruntime.ConfirmKindPermission, Approval: &controlruntime.ApprovalContext{
		Scope:         controlruntime.ApprovalScopeProject,
		ApprovedRules: []protocol.ApprovalRememberRule{{Tool: "shell", Specifier: "curl *"}},
		RememberRules: []protocol.ApprovalRememberRule{{Tool: "shell", Specifier: "python *"}},
	}}, nil)
	desc := cb.formatPermissionDesc()
	if !strings.Contains(desc, "待确认: shell(python *)") || !strings.Contains(desc, "通过: shell(curl *)") {
		t.Fatalf("misleading partial approval:\n%s", desc)
	}
	if strings.Contains(desc, "待确认: shell(curl *)") || strings.Contains(desc, "已授权，本次复用") {
		t.Fatalf("reused grant shown as a decision:\n%s", desc)
	}
	if opts := cb.options(); !hasOptionLabel(opts, "始终允许（记住 shell(python *)）") {
		t.Fatalf("persistent rule not named on the button: %+v", opts)
	}
}

func TestSessionOnlyApprovalOffersNoPersistentOption(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{ToolName: "shell", Args: map[string]any{"command": `curl https://a.test > out.json`}, Kind: controlruntime.ConfirmKindPermission, Approval: &controlruntime.ApprovalContext{
		Scope:         controlruntime.ApprovalScopeProject,
		RememberRules: []protocol.ApprovalRememberRule{{Tool: "shell", Literal: `curl https://a.test > out.json`, Lifetime: "session"}},
	}}, nil)
	desc := cb.formatPermissionDesc()
	if !strings.Contains(desc, "本会话: 整段脚本") || !strings.Contains(desc, "会话结束后失效") {
		t.Fatalf("session grant not explained:\n%s", desc)
	}
	if strings.Contains(desc, "curl https://a.test > out.json") {
		t.Fatalf("full script repeated in the summary:\n%s", desc)
	}
	if strings.Contains(desc, "长期授权生效项目") {
		t.Fatalf("session grant claims a file write:\n%s", desc)
	}
	opts := cb.options()
	if !hasOptionLabel(opts, "本次会话允许") || hasOptionLabel(opts, "始终允许") {
		t.Fatalf("one-off statement offered a permanent grant: %+v", opts)
	}
}

func hasOptionLabel(opts []confirmOption, label string) bool {
	for _, opt := range opts {
		if opt.Label == label {
			return true
		}
	}
	return false
}

func TestMandatoryAskExplainsWhyPermanentApprovalIsUnavailable(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{ToolName: "shell", Args: map[string]any{"command": "curl https://a.test"}, Kind: controlruntime.ConfirmKindPermission, Approval: &controlruntime.ApprovalContext{Scope: controlruntime.ApprovalScopeOnce, Source: "user_rule", Reason: "explicit ask rule requires approval on every call"}}, nil)
	view := cb.View(100, 40)
	if strings.Contains(view, "始终允许") || !strings.Contains(view, "每次确认") {
		t.Fatalf("misleading mandatory approval:\n%s", view)
	}
}

func TestFailedJevApprovalDoesNotClaimACompletedJudgment(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{ToolName: "shell", Args: map[string]any{"command": "make all"}, Kind: controlruntime.ConfirmKindPermission, Approval: &controlruntime.ApprovalContext{Risk: "jev: judgment failed", Reason: "jev: judgment failed", Scope: controlruntime.ApprovalScopeProject}}, nil)
	view := cb.View(100, 40)
	// Stated exactly once, and it must not read as a completed safe verdict.
	if strings.Count(view, "风险判定失败") != 1 || strings.Contains(view, "安全") {
		t.Fatalf("misleading failure status:\n%s", view)
	}
}

// The header answers "why am I being asked?" once. Repeating the same fact as
// a title, a reason and a structure list reads as jargon and tells the user
// nothing more the second and third time.
func TestApprovalHeaderStatesTheReasonOnce(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{
		ToolName: "shell",
		Args:     map[string]any{"command": "x=$(date)\necho \"$x\""},
		Kind:     controlruntime.ConfirmKindPermission,
		Approval: &controlruntime.ApprovalContext{
			Risk:       "dynamic shell execution",
			Reason:     "dynamic shell execution",
			Scope:      controlruntime.ApprovalScopeProject,
			Structures: []string{"command_substitution", "file_redirection"},
		},
	}, nil)
	desc := cb.formatPermissionDesc()
	header := strings.SplitN(desc, "\n", 2)[0]
	if got := strings.Count(desc, "运行时生成内容"); got != 1 {
		t.Fatalf("reason repeated %d times:\n%s", got, desc)
	}
	if header != "原因: 运行时生成内容" {
		t.Fatalf("first line should be the reason field only, got %q", header)
	}
	for _, jargon := range []string{"动态执行结构", "执行结构", "命令替换", "文件写入重定向", "Shell 语法", "需要确认后才能执行"} {
		if strings.Contains(desc, jargon) {
			t.Fatalf("body still carries jargon %q:\n%s", jargon, desc)
		}
	}
}

// The button must name exactly what gets written, and the workspace line must
// appear only for the persistent part: a session-only decision never claims a
// file write.
func TestRememberButtonNamesEveryPersistentRule(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{ToolName: "shell", Args: map[string]any{"command": "npm install && go build"}, Kind: controlruntime.ConfirmKindPermission, Approval: &controlruntime.ApprovalContext{
		Scope:             controlruntime.ApprovalScopeProject,
		RememberWorkspace: "/repo",
		RememberRules: []protocol.ApprovalRememberRule{
			{Tool: "shell", Specifier: "npm *"},
			{Tool: "shell", Specifier: "go *"},
		},
	}}, nil)
	opts := cb.options()
	if !hasOptionLabel(opts, "始终允许（记住 2 条规则）") {
		t.Fatalf("multi-rule decision not summarized on the button: %+v", opts)
	}
	desc := cb.formatPermissionDesc()
	if !strings.Contains(desc, "待确认: shell(npm *, go *)") {
		t.Fatalf("same-tool rules should share one pair of parentheses:\n%s", desc)
	}
	if !strings.Contains(desc, "长期授权生效项目: /repo") {
		t.Fatalf("persistent decision should name the target file scope:\n%s", desc)
	}
}

// A mixed decision offers both scopes: persistent rules can be written, while
// the exact statement only holds for this session.
func TestMixedApprovalOffersPersistentScopeOnly(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	cb.SetRequest(&controlruntime.ConfirmRequest{ToolName: "shell", Args: map[string]any{"command": `npm install && curl https://a.test > out.json`}, Kind: controlruntime.ConfirmKindPermission, Approval: &controlruntime.ApprovalContext{
		Scope:             controlruntime.ApprovalScopeProject,
		RememberWorkspace: "/repo",
		RememberRules: []protocol.ApprovalRememberRule{
			{Tool: "shell", Specifier: "npm *"},
			{Tool: "shell", Literal: `curl https://a.test > out.json`, Lifetime: "session"},
		},
	}}, nil)
	opts := cb.options()
	if !hasOptionLabel(opts, "始终允许（记住 shell(npm *)），并在本会话允许精确脚本") {
		t.Fatalf("persistent part must drive the button label: %+v", opts)
	}
	if hasOptionLabel(opts, "本次会话允许") {
		t.Fatalf("a mixed decision is still persistable; session-only label is wrong: %+v", opts)
	}
	desc := cb.formatPermissionDesc()
	for _, want := range []string{"待确认: shell(npm *)", "本会话: 整段脚本", "长期授权生效项目: /repo"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("mixed approval missing %q:\n%s", want, desc)
		}
	}
}

// Long rule lists collapse; the count is what matters past a few entries.
func TestRuleListCollapsesPastThreeEntries(t *testing.T) {
	rules := []protocol.ApprovalRememberRule{
		{Tool: "shell", Specifier: "a *"},
		{Tool: "shell", Specifier: "b *"},
		{Tool: "shell", Specifier: "c *"},
		{Tool: "shell", Specifier: "d *"},
	}
	got := formatRuleList(rules)
	if !strings.Contains(got, "等 4 项") {
		t.Fatalf("long list should collapse to a count, got %q", got)
	}
	if strings.Contains(got, "d *") {
		t.Fatalf("collapsed list must not spell out the overflow, got %q", got)
	}
	if got := formatRuleList(nil); got != "" {
		t.Fatalf("empty list should render nothing, got %q", got)
	}
}

// Approving must send the exact decision the user highlighted, and the card
// clears itself afterwards so a stale request cannot be resubmitted.
func TestSessionOnlyButtonSendsRemember(t *testing.T) {
	sty := styles.DefaultStyles()
	cb := NewConfirmBar(&sty)
	replies := make(chan controlruntime.ConfirmReply, 1)
	cb.SetRequest(&controlruntime.ConfirmRequest{ToolName: "shell", Args: map[string]any{"command": `curl https://a.test > out.json`}, Kind: controlruntime.ConfirmKindPermission, Approval: &controlruntime.ApprovalContext{
		Scope:         controlruntime.ApprovalScopeProject,
		RememberRules: []protocol.ApprovalRememberRule{{Tool: "shell", Literal: `curl https://a.test > out.json`, Lifetime: "session"}},
	}}, func(ok, remember bool) {
		replies <- controlruntime.ConfirmReply{Allowed: ok, Remember: ok && remember}
	})
	// 0=仅本次允许, 1=本次会话允许, 2=拒绝
	cb.Move(1)
	cb.Submit()
	reply := <-replies
	if !reply.Allowed || !reply.Remember {
		t.Fatalf("session approval should be allowed+remembered: %+v", reply)
	}
	if cb.View(100, 40) != "" {
		t.Fatal("card should clear after submitting")
	}
}
