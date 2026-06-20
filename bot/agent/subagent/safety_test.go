package subagent

import (
	"testing"

	"nekocode/bot/tools"
)

func TestIsSensitiveCallDetectsDangerousBash(t *testing.T) {
	if !isSensitiveCall(tools.ToolCallItem{Name: "bash", Args: map[string]any{"command": "rm -rf /tmp/build"}}) {
		t.Fatal("dangerous bash command should be sensitive")
	}
}

func TestIsSensitiveCallDetectsSensitivePath(t *testing.T) {
	if !isSensitiveCall(tools.ToolCallItem{Name: "read", Args: map[string]any{"path": ".env.local"}}) {
		t.Fatal("sensitive path read should be sensitive")
	}
}

func TestIsSensitiveCallAllowsNormalRead(t *testing.T) {
	if isSensitiveCall(tools.ToolCallItem{Name: "read", Args: map[string]any{"path": "main.go"}}) {
		t.Fatal("normal source read should not be sensitive")
	}
}
