package builtin

import (
	"context"
	"strings"
	"testing"
)

func TestProjectInfoTool(t *testing.T) {
	pi := &ProjectInfoTool{}

	// Without index set, returns informational message (not error).
	out, err := pi.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "not available") {
		t.Logf("output: %s", out)
	}
}
