package protocol

import (
	"context"
	"errors"
	"testing"
)

func TestEvidencePersistenceMarkerPreservesCauseAndDoesNotInferExecution(t *testing.T) {
	cause := errors.New("storage failure")
	marked := MarkEvidenceIncomplete(cause)
	if MarkEvidenceIncomplete(marked) != marked {
		t.Fatal("repeated classification duplicated marker")
	}
	if !errors.Is(marked, cause) || !EvidenceIncomplete(errors.Join(context.Canceled, marked)) {
		t.Fatal("marker lost wrapped cause")
	}
	if MarkEvidenceIncomplete(nil) != nil || EvidenceIncomplete(cause) || EvidenceIncomplete(errors.New(marked.Error())) {
		t.Fatal("incompleteness inferred from display text or ordinary error")
	}
}
