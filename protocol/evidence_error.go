package protocol

import (
	"errors"
	"fmt"
)

// ErrIncompleteResponse identifies a provider response that cannot finish the model turn.
var ErrIncompleteResponse = errors.New("model response incomplete")

// OnlyFailure reports whether every cause is the same terminal condition.
// Persistence failures always retain execution-error priority.
func OnlyFailure(err, target error) bool {
	if err == nil || EvidenceIncomplete(err) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		for _, child := range children {
			if !OnlyFailure(child, target) {
				return false
			}
		}
		return len(children) > 0
	}
	if child := errors.Unwrap(err); child != nil {
		return OnlyFailure(child, target)
	}
	return errors.Is(err, target)
}

// OnlyIncompleteResponse reports a model response failure without a separate execution error.
func OnlyIncompleteResponse(err error) bool { return OnlyFailure(err, ErrIncompleteResponse) }

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
