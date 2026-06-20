package netclient

import "testing"

func TestNewHTTPClient(t *testing.T) {
	c := NewHTTPClient(0)
	if c == nil {
		t.Fatal("nil client")
	}
	if c.Timeout != 0 {
		t.Fatal("expected zero timeout")
	}
}
