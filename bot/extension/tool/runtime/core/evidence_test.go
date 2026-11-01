package core

import "testing"

func TestOutputHashPreservesInvalidUTF8AndIgnoresReadDeliveryIDs(t *testing.T) {
	first := ToolOutput{Text: string([]byte{0xff}), ReadID: "one"}
	second := first
	second.ReadID = "two"
	if OutputHash(first) != OutputHash(second) {
		t.Fatal("ephemeral read receipt changed boundary digest")
	}
	second.Text = string([]byte{0xfe})
	if OutputHash(first) == OutputHash(second) {
		t.Fatal("JSON replacement merged distinct raw output bytes")
	}
}
