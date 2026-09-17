package headless

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	rt "nekocode/runtime"
)

type stdioTestBackend struct{ rejectingStartBackend }

func (stdioTestBackend) NewSession() (rt.SessionMeta, error) {
	return rt.SessionMeta{ID: "session-test"}, nil
}

func (stdioTestBackend) StartRun(context.Context, rt.Input) (rt.RunID, error) {
	if os.Getenv("NEKOCODE_STREAM_TEST_HELPER") == "blocked-output" {
		return "", errors.New(strings.Repeat("x", 256*1024))
	}
	return "", errors.New("start rejected")
}

func TestStdioProcessHelper(t *testing.T) {
	mode := os.Getenv("NEKOCODE_STREAM_TEST_HELPER")
	if mode == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	options := Options{InputFormat: "stream-json"}
	if mode == "prompt" || mode == "blocked-output" {
		options.Prompt = "run"
	}
	err := serveStdio(ctx, stdioTestBackend{}, "/workspace", options)
	if mode == "prompt" && err != nil {
		t.Fatal(err)
	}
	if mode != "prompt" && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	// The real CLI exits without writing another banner. In the blocked-output
	// case the testing package's PASS banner would itself block on stdout after
	// Serve has correctly restored its original blocking mode.
	os.Exit(0)
}

func TestStdioProcessExitsWithOpenInput(t *testing.T) {
	for _, mode := range []string{"prompt", "cancel", "blocked-output"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestStdioProcessHelper$")
			cmd.Env = append(os.Environ(), "NEKOCODE_STREAM_TEST_HELPER="+mode)
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if mode == "blocked-output" {
				output, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				defer output.Close()
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-done
				t.Fatal("stdio process hung with stdin still open")
			}
		})
	}
}
