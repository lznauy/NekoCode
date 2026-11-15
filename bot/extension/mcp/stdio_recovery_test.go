package mcp

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

type recoveryWriter struct{ io.Writer }

func (recoveryWriter) Close() error { return nil }

func TestStdioRecoversFromInvalidAndStaleFrames(t *testing.T) {
	for _, prefix := range []string{
		"server log\n", "{bad json}\n", "null\n",
		`{"jsonrpc":"2.0","id":99,"result":{}}` + "\n",
		`{"jsonrpc":"2.0","id":"1","result":{}}` + "\n",
	} {
		t.Run(prefix, func(t *testing.T) {
			c := &client{stdin: recoveryWriter{io.Discard}, stdout: bufio.NewReader(strings.NewReader(prefix +
				`{"jsonrpc":"2.0","id":1,"result":{"ok":1}}` + "\n" +
				`{"jsonrpc":"2.0","id":2,"result":{"ok":2}}` + "\n"))}
			for _, want := range []string{`{"ok":1}`, `{"ok":2}`} {
				got, err := c.sendRequest(context.Background(), "tools/list", nil)
				if err != nil || string(got) != want {
					t.Fatalf("result=%s err=%v, want %s", got, err, want)
				}
			}
		})
	}
}

func TestStdioBatchAndStringServerRequest(t *testing.T) {
	var written bytes.Buffer
	c := &client{stdin: recoveryWriter{&written}, stdout: bufio.NewReader(strings.NewReader(
		`[{"jsonrpc":"2.0","method":"ping","id":"server-id"},{"jsonrpc":"2.0","method":"notifications/progress"},{"jsonrpc":"2.0","id":1,"result":{"ok":true}}]` + "\n"))}
	got, err := c.sendRequest(context.Background(), "tools/list", nil)
	if err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("result=%s err=%v", got, err)
	}
	if !strings.Contains(written.String(), `"id":"server-id"`) || !strings.Contains(written.String(), `"code":-32601`) {
		t.Fatalf("server request not rejected with original ID: %s", written.String())
	}
}

func TestStdioFrameLimit(t *testing.T) {
	_, err := readStdioFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", maxStdioFrameBytes+1))))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized frame error=%v", err)
	}
}

func TestStdioBoundsInvalidFrames(t *testing.T) {
	c := &client{stdin: recoveryWriter{io.Discard}, stdout: bufio.NewReader(strings.NewReader(strings.Repeat("noise\n", maxSkippedStdioFrames+1)))}
	_, err := c.sendRequest(t.Context(), "tools/list", nil)
	if err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("noise error=%v", err)
	}
}

func TestStdioHandlesRequestAfterResponseInBatch(t *testing.T) {
	var written bytes.Buffer
	c := &client{stdin: recoveryWriter{&written}, stdout: bufio.NewReader(strings.NewReader(
		`[{"jsonrpc":"2.0","id":1,"result":{}},{"jsonrpc":"2.0","method":"unsupported","id":"after-response"}]` + "\n"))}
	if _, err := c.sendRequest(t.Context(), "tools/list", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(written.String(), `"id":"after-response"`) {
		t.Fatalf("lost server request: %s", written.String())
	}
}
