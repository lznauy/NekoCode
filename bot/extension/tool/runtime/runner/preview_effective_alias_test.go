package runner

import (
	"context"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"testing"
)

func TestEffectiveAliasRetargetDiscardsPreview(t *testing.T) {
	root := t.TempDir()
	original, replacement, alias := filepath.Join(root, "original"), filepath.Join(root, "replacement"), filepath.Join(root, "alias")
	for _, path := range []string{original, replacement} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(original, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	providers, emitted := 0, 0
	e, call, _ := effectiveFilePreviewFixture(t, root, filepath.Join(alias, "file"), true, func(context.Context, map[string]any) string {
		providers++
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(replacement, alias); err != nil {
			t.Fatal(err)
		}
		return privatePreviewMarker
	})
	e.SetPreviewDecisionFn(func(string, string, map[string]any, string, protocol.ToolDecision) { emitted++ })
	e.emitPreview(t.Context(), call)
	if providers != 1 || emitted != 0 || call.Args["_preview"] != nil {
		t.Fatalf("changed source alias lost its binding: providers=%d emitted=%d args=%v", providers, emitted, call.Args)
	}
}

func TestEffectivePreviewProviderCannotMutateCallerPayload(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	e, call, payload := effectiveFilePreviewFixture(t, root, path, true, func(_ context.Context, args map[string]any) string {
		args["payload"].(map[string]any)["path"] = "changed by provider"
		return "preview"
	})
	e.PreparePreviewsContext(t.Context(), []core.ToolCallItem{call})
	if payload["path"] != path {
		t.Fatalf("provider mutated executable proxy payload: %+v", payload)
	}
}
