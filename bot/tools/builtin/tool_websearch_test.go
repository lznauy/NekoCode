package builtin

import (
	"context"
	"testing"
)

func TestWebSearchTool(t *testing.T) {
	ws := &WebSearchTool{}
	_, err := ws.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing query")
	}
}
