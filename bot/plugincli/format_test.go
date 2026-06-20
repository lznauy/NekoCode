package plugincli

import (
	"testing"
)

func TestExpandPluginEnv(t *testing.T) {
	got := ExpandPluginEnv(map[string]string{"A": "${PLUGIN_ROOT}/bin", "B": "${CLAUDE_PLUGIN_ROOT}/lib"}, "/tmp/p")
	if got["A"] != "/tmp/p/bin" || got["B"] != "/tmp/p/lib" {
		t.Fatalf("expanded env = %#v", got)
	}
}
