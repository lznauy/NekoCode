package core

import (
	"context"
	"testing"
)

func TestBotRejectsOverlappingOperationsBeforeBindingHost(t *testing.T) {
	b := &Bot{}
	b.operationMu.Lock()
	defer b.operationMu.Unlock()
	if _, err := b.Run(context.Background(), "", nil, nil); err == nil {
		t.Fatal("concurrent Run accepted")
	}
	if _, err := b.ExecuteCommand(context.Background(), "", nil); err == nil {
		t.Fatal("concurrent command accepted")
	}
}
