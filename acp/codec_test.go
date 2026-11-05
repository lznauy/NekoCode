package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodec(t *testing.T) {
	in := strings.NewReader("not-json\n" + `{"jsonrpc":"2.0","id":7,"method":"echo","params":{"text":"hi"}}` + "\n")
	var out bytes.Buffer
	c := newConnection(&out)
	err := c.serve(context.Background(), in, func(_ context.Context, method string, params json.RawMessage) (any, *wireError) {
		if method != "echo" {
			t.Fatalf("method = %q", method)
		}
		return map[string]string{"text": "hi"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("responses = %q", out.String())
	}
	var response message
	if err := json.Unmarshal([]byte(lines[1]), &response); err != nil {
		t.Fatal(err)
	}
	if string(response.ID) != "7" || string(response.Result) != `{"text":"hi"}` {
		t.Fatalf("response = %#v", response)
	}
}

func TestCodecOverloadDoesNotBlockCancel(t *testing.T) {
	var input strings.Builder
	for i := 0; i < maxConcurrentRequests+1; i++ {
		fmt.Fprintf(&input, `{"jsonrpc":"2.0","id":%d,"method":"slow"}`+"\n", i+1)
	}
	input.WriteString(`{"jsonrpc":"2.0","method":"session/cancel"}` + "\n")

	release := make(chan struct{})
	var releaseOnce sync.Once
	conn := newConnection(&bytes.Buffer{})
	done := make(chan error, 1)
	go func() {
		done <- conn.serve(context.Background(), strings.NewReader(input.String()), func(_ context.Context, method string, _ json.RawMessage) (any, *wireError) {
			if method == "session/cancel" {
				releaseOnce.Do(func() { close(release) })
				return struct{}{}, nil
			}
			<-release
			return struct{}{}, nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		releaseOnce.Do(func() { close(release) })
		t.Fatal("reader stopped before processing session/cancel")
	}
}

type blockingWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(p), nil
}

func TestCodecBlockedOutputDoesNotBlockCancel(t *testing.T) {
	var input strings.Builder
	for i := 0; i < maxConcurrentRequests+1; i++ {
		fmt.Fprintf(&input, `{"jsonrpc":"2.0","id":%d,"method":"slow"}`+"\n", i+1)
	}
	input.WriteString(`{"jsonrpc":"2.0","method":"session/cancel"}` + "\n")

	writer := &blockingWriter{started: make(chan struct{}), release: make(chan struct{})}
	releaseHandlers := make(chan struct{})
	var once sync.Once
	conn := newConnection(writer)
	done := make(chan error, 1)
	go func() {
		done <- conn.serve(context.Background(), strings.NewReader(input.String()), func(_ context.Context, method string, _ json.RawMessage) (any, *wireError) {
			if method == "session/cancel" {
				once.Do(func() { close(releaseHandlers) })
				return struct{}{}, nil
			}
			<-releaseHandlers
			return struct{}{}, nil
		})
	}()
	defer close(writer.release)

	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("writer was not exercised")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		once.Do(func() { close(releaseHandlers) })
		t.Fatal("blocked output prevented cancellation from being read")
	}
}

func TestRawIDPreservesLargeIntegers(t *testing.T) {
	// 2^63-1 must survive the round trip without float64 rounding; a naive
	// decode to any would round it to 9223372036854775808.
	const id = `9223372036854775807`
	value := rawID(json.RawMessage(id))
	number, ok := value.(json.Number)
	if !ok || number.String() != id {
		t.Fatalf("rawID = %#v", value)
	}
	encoded, err := json.Marshal(map[string]any{"id": value})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"id":`+id) {
		t.Fatalf("encoded = %s", encoded)
	}
	if rawID(nil) != nil {
		t.Fatal("empty id must stay nil")
	}
	if got := rawID(json.RawMessage(`"abc"`)); got != "abc" {
		t.Fatalf("string id = %#v", got)
	}
}

// TestCodecOverloadWithFullWriteQueueStopsReading covers the overload path
// where the concurrency slots and the write queue are both exhausted. The
// rejected request must never be dispatched (its slot release would block
// forever) and the read loop must terminate so serve can return.
func TestCodecOverloadWithFullWriteQueueStopsReading(t *testing.T) {
	pipeIn, pipeWriter := io.Pipe()
	release := make(chan struct{})
	overloadCalled := make(chan string, 1)

	writer := &blockingWriter{started: make(chan struct{}), release: make(chan struct{})}
	conn := newConnection(writer)
	done := make(chan error, 1)
	go func() {
		done <- conn.serve(context.Background(), pipeIn, func(_ context.Context, method string, _ json.RawMessage) (any, *wireError) {
			if method == "overload" {
				select {
				case overloadCalled <- method:
				default:
				}
				return struct{}{}, nil
			}
			select {
			case <-release:
			case <-time.After(10 * time.Second):
			}
			return struct{}{}, nil
		})
	}()

	// Exhaust the request slots.
	for i := 0; i < maxConcurrentRequests; i++ {
		fmt.Fprintf(pipeWriter, `{"jsonrpc":"2.0","id":%d,"method":"slow"}`+"\n", i+1)
	}
	// Block the write loop on its first write, then fill the write queue with
	// parse-error responses (they are queued without waiting for delivery).
	fmt.Fprint(pipeWriter, "not-json\n")
	select {
	case <-writer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("write loop never started")
	}
	for i := 0; i < maxQueuedWrites; i++ {
		fmt.Fprint(pipeWriter, "not-json\n")
	}

	// This request is rejected while the write queue is full: the rejection
	// response cannot be delivered, so the read loop must stop instead of
	// dispatching it.
	fmt.Fprint(pipeWriter, `{"jsonrpc":"2.0","id":999,"method":"overload"}`+"\n")
	pipeWriter.Close()

	select {
	case method := <-overloadCalled:
		t.Fatalf("overloaded request %q was dispatched to a handler", method)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	close(writer.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after overload rejection")
	}
}

func TestCodecCancellationInterruptsInput(t *testing.T) {
	in, peer := io.Pipe()
	defer in.Close()
	defer peer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- newConnection(io.Discard).serve(ctx, in, func(context.Context, string, json.RawMessage) (any, *wireError) { return nil, nil })
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt input read")
	}
}

func TestCodecWriteFailureInterruptsInput(t *testing.T) {
	in, peer := io.Pipe()
	defer in.Close()
	defer peer.Close()
	done := make(chan error, 1)
	go func() {
		done <- newConnection(errorWriter{}).serve(context.Background(), in, func(context.Context, string, json.RawMessage) (any, *wireError) { return nil, nil })
	}()
	if _, err := io.WriteString(peer, "invalid JSON\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "write message") {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("write failure did not interrupt input read")
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
