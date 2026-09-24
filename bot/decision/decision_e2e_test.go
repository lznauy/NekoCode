package decision

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestJevEndToEnd exercises the real /v1/systemone endpoint. It is skipped
// unless TYPESAFE_E2E_KEY is set, so normal test runs never touch the
// network.
func TestJevEndToEnd(t *testing.T) {
	key := os.Getenv("TYPESAFE_E2E_KEY")
	if key == "" {
		t.Skip("TYPESAFE_E2E_KEY not set; skipping live Jev call")
	}
	scorer := NewToolPruner(JevSettings{APIKey: key, Timeout: 10 * time.Second})
	if scorer == nil {
		t.Fatal("pruner should be constructed with a key")
	}
	prune, ok := scorer.PruneTools(context.Background(), "fix a login bug in a Go service", []Candidate{
		{Tool: "shell", Head: "build failed: cannot find package nekocode/login"},
		{Tool: "read", Head: "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }"},
	})
	if !ok {
		t.Fatal("live PruneTools call failed")
	}
	if len(prune) != 2 {
		t.Fatalf("prune = %v, want two verdicts", prune)
	}
	t.Logf("verdicts: build-failure keep=%v file-content keep=%v", !prune[0], !prune[1])
}
