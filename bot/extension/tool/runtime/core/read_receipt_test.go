package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReadReceiptNeverSerializes(t *testing.T) {
	for _, value := range []any{
		ToolOutput{ReadID: "local-receipt", Text: "visible"},
		ToolCallResult{ReadID: "local-receipt", Output: "visible"},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "local-receipt") || strings.Contains(string(data), "ReadID") {
			t.Fatalf("local receipt escaped: %s", data)
		}
		if !strings.Contains(string(data), "visible") {
			t.Fatal("public output was lost")
		}
	}
}
