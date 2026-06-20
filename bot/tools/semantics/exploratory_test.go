package semantics

import (
	"testing"

	"nekocode/bot/tools/core"
)

func TestIsAllExploratory(t *testing.T) {
	if IsAllExploratory(nil) {
		t.Fatal("empty calls should not be exploratory")
	}
	if !IsAllExploratory([]core.ToolCallItem{{Name: "read"}, {Name: "web_fetch"}}) {
		t.Fatal("expected read/web_fetch to be exploratory")
	}
	if IsAllExploratory([]core.ToolCallItem{{Name: "read"}, {Name: "write"}}) {
		t.Fatal("write should not be exploratory")
	}
}
