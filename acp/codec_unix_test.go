//go:build unix

package acp

import (
	"context"
	"encoding/json"
	"golang.org/x/sys/unix"
	"io"
	"nekocode/util/stdio"
	"os"
	"testing"
	"time"
)

func TestCodecCancellationInterruptsInheritedPipe(t *testing.T) {
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	original := os.NewFile(uintptr(fds[0]), "inherited-input")
	peer := os.NewFile(uintptr(fds[1]), "input-peer")
	defer original.Close()
	defer peer.Close()
	in, restore, err := stdio.Duplicate(original)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	defer in.Close()
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
		t.Fatal("cancellation did not interrupt inherited pipe")
	}
}
