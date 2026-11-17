package deliver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidConnectorNeverFallsBackToCurrentSource(t *testing.T) {
	ctx, dir := wsContext(t)
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("data"), 0600)
	for _, v := range []any{nil, 42, []any{"telegram"}, " "} {
		backend := &fakeDeliverer{sent: []string{"current"}}
		if _, err := NewTool(backend).Execute(ctx, map[string]any{"path": path, "connector": v}); err == nil {
			t.Fatalf("invalid connector accepted: %#v", v)
		}
		if backend.path != "" {
			t.Fatalf("invalid connector caused delivery: %#v", v)
		}
	}
}
