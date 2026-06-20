package pluginruntime

import "testing"

func TestIsMCPToolForClient(t *testing.T) {
	if !IsMCPToolForClient("srv__tool", "srv") {
		t.Fatal("expected MCP tool match")
	}
	if IsMCPToolForClient("srv2__tool", "srv") {
		t.Fatal("should not match another server prefix")
	}
	if IsMCPToolForClient("srv_tool", "srv") {
		t.Fatal("should require double underscore separator")
	}
}
