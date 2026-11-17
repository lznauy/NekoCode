package search

import (
	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
	"testing"
)

func TestGrepRejectsInvalidOptionalFilters(t *testing.T) {
	dir := testutil.SetupTemp(t)
	for _, key := range []string{"path", "glob", "context_lines"} {
		for _, v := range []any{nil, 42, []any{"*.go"}} {
			args := map[string]any{"pattern": "package", "path": dir}
			args[key] = v
			if _, err := (&GrepTool{}).Execute(testutil.Context(dir), args); err == nil {
				t.Fatalf("invalid %s accepted: %#v", key, v)
			}
		}
	}
}
