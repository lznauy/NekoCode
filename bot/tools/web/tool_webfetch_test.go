package web

import (
	"context"
	"testing"
)

func TestWebFetchTool(t *testing.T) {
	wf := &WebFetchTool{}
	_, err := wf.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing url")
	}
}
