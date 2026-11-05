//go:build linux

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestImageFIFOAllowsCancellationToSettle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fifo := filepath.Join(t.TempDir(), "upload.png")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 1)
	bot := &sessionCommandRunner{current: "session_1"}
	services := sessionRunnerServices(bot)
	services.Sessions.CurrentSessionID = func() string {
		select {
		case entered <- struct{}{}:
		default:
		}
		return "session_1"
	}
	rt := New(bot, services)
	settled := false
	defer func() {
		if !settled {
			for until := time.Now().Add(time.Second); time.Now().Before(until); {
				f, e := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0600)
				if e == nil {
					_, _ = f.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
					_ = f.Close()
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
		_ = rt.Close()
	}()
	id, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "headless"}, Text: "[Image #1]", Images: []ImageAttachment{{Label: "[Image #1]", Path: fifo}}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	time.Sleep(20 * time.Millisecond)
	if err := rt.CancelRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := rt.WaitRun(ctx, id); err != nil {
		t.Fatalf("FIFO image blocks run cleanup after cancellation: %v", err)
	}
	settled = true
}
