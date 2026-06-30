package semantics

import "testing"

func TestClassifyBashExplorationAndVerification(t *testing.T) {
	sem := ClassifyToolCall("bash", map[string]any{"command": "cat README.md"})
	if !sem.Exploratory || !sem.SourceProducing {
		t.Fatalf("cat should be exploratory source-producing: %+v", sem)
	}

	sem = ClassifyToolCall("bash", map[string]any{"command": "go test ./..."})
	if !sem.Verifying || sem.Exploratory {
		t.Fatalf("go test should be verifying, not exploratory: %+v", sem)
	}
}

func TestClassifyBashVerificationIsNotMutation(t *testing.T) {
	for _, cmd := range []string{"go test ./...", "make test", "npm run lint"} {
		sem := ClassifyToolCall("bash", map[string]any{"command": cmd})
		if !sem.Verifying || sem.Mutating {
			t.Fatalf("%q should be verifying without mutating: %+v", cmd, sem)
		}
	}
}

func TestClassifyMutation(t *testing.T) {
	for _, name := range []string{"write", "edit"} {
		sem := ClassifyToolCall(name, nil)
		if !sem.Mutating {
			t.Fatalf("%s should be mutating: %+v", name, sem)
		}
	}
}
