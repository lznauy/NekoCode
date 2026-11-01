package protocol

import (
	"context"
	"testing"
)

func TestExecutionScopesAreImmutableAndPreserveCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	root := WithExecutionIdentity(parent, ExecutionIdentity{RunID: "run"})
	child := WithExecutionIdentity(root, ExecutionIdentity{RunID: "run", TaskID: "task", AttemptID: "attempt"})
	copy := ExecutionFromContext(child)
	copy.RunID = "other"
	if ExecutionFromContext(root) != (ExecutionIdentity{RunID: "run"}) || ExecutionFromContext(child).RunID != "run" {
		t.Fatal("child or reader mutated an existing execution scope")
	}
	cancel()
	if child.Err() != context.Canceled || ExecutionFromContext(context.WithoutCancel(child)).AttemptID != "attempt" {
		t.Fatal("execution values changed context cancellation semantics")
	}
	if ExecutionFromContext(nil) != (ExecutionIdentity{}) || ExecutionFromContext(t.Context()) != (ExecutionIdentity{}) {
		t.Fatal("unbound context acquired an execution identity")
	}
}
