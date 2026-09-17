package headless

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestCodecReadsChunkedFramesAndFinalLine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, writer := io.Pipe()
	defer in.Close()
	transport := newTransport(ctx, in, io.Discard)
	go func() {
		for _, chunk := range []string{`{"type":`, `"keep_alive"}`, "\n", `{"type":"keep_alive"}`} {
			_, _ = io.WriteString(writer, chunk)
		}
		_ = writer.Close()
	}()
	for i := 0; i < 2; i++ {
		select {
		case f := <-transport.reads:
			if f.err != nil || f.frame.Type != "keep_alive" {
				t.Fatal(f)
			}
		case <-time.After(time.Second):
			t.Fatal("blocked")
		}
	}
	if f := <-transport.reads; f.err != io.EOF {
		t.Fatal(f)
	}
	cancel()
	transport.wg.Wait()
}

func TestCodecRejectsOversizedInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := newTransport(ctx, strings.NewReader(strings.Repeat("x", maxFrameSize+1)), io.Discard)
	f := <-transport.reads
	if f.err == nil || f.err == io.EOF {
		t.Fatal("oversized input accepted")
	}
	cancel()
	transport.wg.Wait()
}

func TestSlowWriterFailsExplicitlyAndCanBeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	transport := newTransport(ctx, strings.NewReader(""), writer)
	defer func() { cancel(); _ = writer.Close(); _ = reader.Close(); transport.wg.Wait() }()
	var err error
	for i := 0; i < 1000; i++ {
		if err = transport.send(map[string]any{"type": "keep_alive"}); err != nil {
			break
		}
	}
	if err == nil || !strings.Contains(err.Error(), "queue overflow") {
		t.Fatalf("slow writer error: %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestShortWriteFailsFlush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	transport := newTransport(ctx, strings.NewReader(""), shortWriter{})
	if err := transport.send(map[string]any{"type": "keep_alive"}); err != nil {
		t.Fatal(err)
	}
	if err := transport.flush(ctx); err != io.ErrShortWrite {
		t.Fatalf("flush: %v", err)
	}
	cancel()
	transport.wg.Wait()
}

type countedResponse struct{ calls int }

func (r *countedResponse) MarshalJSON() ([]byte, error) {
	r.calls++
	return []byte(`{"value":"ok"}`), nil
}

func TestControlResponseEncodesBodyOnce(t *testing.T) {
	body := &countedResponse{}
	s := &server{conn: &transport{ctx: context.Background(), writes: make(chan writeItem, 1)}}
	if err := s.respond("request", body, nil); err != nil {
		t.Fatal(err)
	}
	if body.calls != 1 {
		t.Fatalf("encoded body %d times", body.calls)
	}
	item := <-s.conn.writes
	if !strings.Contains(string(item.data), `"response":{"value":"ok"}`) || item.data[len(item.data)-1] != '\n' {
		t.Fatalf("invalid response frame: %s", item.data)
	}
}
