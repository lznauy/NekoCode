package protocol

import (
	"errors"
	"fmt"
)

// MarkEvidenceIncomplete preserves the storage error without claiming that a
// tool ran, failed, or can be replayed. It is also valid for partial commits.
func MarkEvidenceIncomplete(err error) error {
	if err == nil || EvidenceIncomplete(err) {
		return err
	}
	return &evidencePersistenceError{err}
}

type evidencePersistenceError struct{ err error }

func (e *evidencePersistenceError) Error() string {
	return fmt.Sprintf("execution evidence may be incomplete; do not replay tools automatically: %v", e.err)
}
func (e *evidencePersistenceError) Unwrap() error { return e.err }

// EvidenceIncomplete reads a structured error marker, never display text.
func EvidenceIncomplete(err error) bool {
	var marked *evidencePersistenceError
	return errors.As(err, &marked) && marked != nil
}
