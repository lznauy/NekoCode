package index

import (
	"testing"

	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestIndexRejectsCrossProjectReuseAndCloses(t *testing.T) {
	tool := NewIndexTool()
	first := workspace.WithManager(t.Context(), workspace.New(t.TempDir(), nil))
	second := workspace.WithManager(t.Context(), workspace.New(t.TempDir(), nil))
	if _, err := tool.ensureInit(first); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.ensureInit(second); err == nil {
		t.Fatal("cross-project index reuse accepted")
	}
	if err := tool.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.ensureInit(first); err == nil {
		t.Fatal("closed index reused")
	}
}
