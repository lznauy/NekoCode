package components

import (
	"strings"
	"testing"

	"nekocode/interaction/tui/styles"
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
	for _, want := range []string{"需要临时授权", "主机执行", "仅本次", "/repo"} {
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
			Risk:         "dynamic shell execution",
			Reason:       "command requests sandbox profile: net.outbound",
			Capabilities: []string{"net.outbound"},
			Scope:        controlruntime.ApprovalScopeProject,
			Structures:   []string{"command_substitution", "shell_command_string"},
			WritePaths:   []string{"/tmp/cache"},
			Combined:     true,
		},
	}, nil)

	view := cb.View(100, 40)
	for _, want := range []string{"执行确认", `bash -c`, "命令替换", "Shell -c", "出站网络", "/tmp/cache", "始终允许"} {
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
	for _, want := range []string{"执行确认", "动态执行结构", "动态命令名"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dynamic approval missing %q:\n%s", want, view)
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
	for _, want := range []string{"Jev 判定该命令存在风险", "※ 本命令经过 Jev 判定，需要授权确认"} {
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
