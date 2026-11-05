package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

type dispatchWriter struct{ bytes.Buffer }

func (*dispatchWriter) Close() error { return nil }

func TestClientDispatchInterleavedMessages(t *testing.T) {
	var wire bytes.Buffer
	encode := json.NewEncoder(&wire)
	for id := 1; id <= 3; id++ {
		_ = encode.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"data": "hello"}})
		_ = encode.Encode(map[string]any{"jsonrpc": "2.0", "id": "server-request", "method": "sampling/createMessage"})
		var result any = map[string]any{"tools": []any{}}
		if id == 1 {
			result = map[string]any{"protocolVersion": "2024-11-05"}
		}
		_ = encode.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	writer := &dispatchWriter{}
	c := &client{stdin: writer, stdout: bufio.NewReader(&wire)}
	if err := c.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.sendRequest(context.Background(), "tools/list", nil); err != nil {
			t.Fatal(err)
		}
	}
	decoder := json.NewDecoder(&writer.Buffer)
	responses := 0
	for decoder.More() {
		var msg struct {
			ID    json.RawMessage `json:"id"`
			Error *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := decoder.Decode(&msg); err != nil {
			t.Fatal(err)
		}
		if msg.Error != nil {
			if string(msg.ID) != `"server-request"` || msg.Error.Code != -32601 {
				t.Fatalf("unexpected server request response: %+v", msg)
			}
			responses++
		}
	}
	if responses != 3 {
		t.Fatalf("unsupported responses = %d, want 3", responses)
	}
}
