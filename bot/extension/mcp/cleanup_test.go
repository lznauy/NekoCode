package mcp

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type delayedCleanupWriter struct{ started, closing, finished chan struct{} }

func (w *delayedCleanupWriter) Write(data []byte) (int, error) {
	close(w.started)
	<-w.closing
	time.Sleep(20 * time.Millisecond)
	close(w.finished)
	return 0, io.ErrClosedPipe
}
func (w *delayedCleanupWriter) Close() error {
	select {
	case <-w.closing:
	default:
		close(w.closing)
	}
	return nil
}

func TestCancelledWriteJoinsWriter(t *testing.T) {
	writer := &delayedCleanupWriter{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	c := &client{stdin: writer}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- c.writeMessage(ctx, make(chan time.Time), "test", time.Second, []byte("request")) }()
	<-writer.started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error lost: %v", err)
		}
		select {
		case <-writer.finished:
		default:
			writer.Close()
			<-writer.finished
			t.Fatal("write returned before writer finished")
		}
	case <-time.After(time.Second):
		writer.Close()
		t.Fatal("writer cancellation did not finish")
	}
}

type uninterruptibleCleanupWriter struct{ started, release chan struct{} }

func (w *uninterruptibleCleanupWriter) Write(data []byte) (int, error) {
	close(w.started)
	<-w.release
	return len(data), nil
}
func (*uninterruptibleCleanupWriter) Close() error { return nil }

func TestUninterruptibleWriteCleanupIsBoundedAndRetryable(t *testing.T) {
	writer := &uninterruptibleCleanupWriter{make(chan struct{}), make(chan struct{})}
	defer func() {
		select {
		case <-writer.release:
		default:
			close(writer.release)
		}
	}()
	c := &client{stdin: writer}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- c.writeMessage(ctx, make(chan time.Time), "test", time.Second, []byte("request")) }()
	<-writer.started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "stdin writer did not stop") {
			t.Fatalf("missing cancellation/cleanup failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("non-interruptible writer hung cleanup")
	}
	if !c.stopping || c.writePending == nil {
		t.Fatal("unfinished writer was forgotten")
	}
	close(writer.release)
	if err := c.Close(); err != nil {
		t.Fatalf("cleanup could not be retried: %v", err)
	}
}

type uninterruptibleCleanupReader struct{ release chan struct{} }

func (r *uninterruptibleCleanupReader) Read([]byte) (int, error) { <-r.release; return 0, io.EOF }
func (*uninterruptibleCleanupReader) Close() error               { return nil }

func TestUninterruptibleReadCleanupIsBoundedAndRetryable(t *testing.T) {
	reader := &uninterruptibleCleanupReader{make(chan struct{})}
	defer func() {
		select {
		case <-reader.release:
		default:
			close(reader.release)
		}
	}()
	c := &client{stdin: recoveryWriter{io.Discard}, stdout: bufio.NewReader(reader), stdoutPipe: reader}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.sendRequest(ctx, "blocked", nil); result <- err }()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "stdout reader did not stop") {
			t.Fatalf("missing cancellation/cleanup failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("non-interruptible reader hung cleanup")
	}
	if !c.stopping || c.readPending == nil {
		t.Fatal("unfinished reader was forgotten")
	}
	close(reader.release)
	if err := c.Close(); err != nil {
		t.Fatalf("cleanup retry failed: %v", err)
	}
}
