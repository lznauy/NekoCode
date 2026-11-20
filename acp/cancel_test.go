package acp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCancel(t *testing.T) {
	backend := &fakeBackend{}
	cancelled := false
	s := &server{backend: backend, active: map[string]*activeTurn{
		"session-1": {runID: "run-1", cancel: func() { cancelled = true }},
	}}
	if rpcErr := s.cancel(context.Background(), json.RawMessage(`{"sessionId":"session-1"}`)); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if !cancelled || backend.cancelled != "run-1" {
		t.Fatalf("cancelled = %v, runID = %q", cancelled, backend.cancelled)
	}
}

func TestCancelBeforeRunAssignment(t *testing.T) {
	backend := &fakeBackend{cancelled: "untouched"}
	cancelled := false
	s := &server{backend: backend, active: map[string]*activeTurn{
		"session-1": {cancel: func() { cancelled = true }},
	}}
	if err := s.cancel(t.Context(), json.RawMessage(`{"sessionId":"session-1"}`)); err != nil {
		t.Fatal(err)
	}
	if !cancelled || backend.cancelled != "untouched" {
		t.Fatalf("local cancellation=%v backend cancellation=%q", cancelled, backend.cancelled)
	}
}
