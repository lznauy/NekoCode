package httpapi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	controlruntime "nekocode/runtime"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type admissionRuntime struct {
	*fakeRuntime
	start func(context.Context, controlruntime.Input) (controlruntime.RunID, error)
}

func (r *admissionRuntime) StartRun(ctx context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	return r.start(ctx, input)
}

func TestInputRejectsCancellationBeforeAdmission(t *testing.T) {
	called := false
	rt := &admissionRuntime{fakeRuntime: newFakeRuntime(), start: func(context.Context, controlruntime.Input) (controlruntime.RunID, error) {
		called = true
		return "ghost", nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"task"}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	New(rt).Handler().ServeHTTP(response, request)
	if called || response.Code != http.StatusRequestTimeout {
		t.Fatalf("called=%v status=%d", called, response.Code)
	}
}

func TestInputPreservesAdmissionDeadlineAndCancellation(t *testing.T) {
	for _, expired := range []bool{false, true} {
		var seen context.Context
		rt := &admissionRuntime{fakeRuntime: newFakeRuntime(), start: func(ctx context.Context, _ controlruntime.Input) (controlruntime.RunID, error) {
			seen = ctx
			<-ctx.Done()
			return "", ctx.Err()
		}}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		if expired {
			cancel()
		}
		defer cancel()
		response := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			defer close(done)
			New(rt).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"task"}`)).WithContext(ctx))
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("HTTP admission lost cancellation/deadline")
		}
		if expired {
			if seen != nil {
				t.Fatal("already cancelled input reached StartRun")
			}
		} else if seen == nil || !errors.Is(seen.Err(), context.DeadlineExceeded) || response.Code != http.StatusGatewayTimeout {
			t.Fatalf("ctx=%v status=%d", seen, response.Code)
		}
	}
}

func TestInputAdmissionHasServerTimeout(t *testing.T) {
	rt := &admissionRuntime{fakeRuntime: newFakeRuntime(), start: func(ctx context.Context, _ controlruntime.Input) (controlruntime.RunID, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	server := New(rt)
	server.inputAdmissionTimeout = 20 * time.Millisecond
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"task"}`)))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server admission timeout not enforced")
	}
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestAcceptedHTTPRunSurvivesHandlerReturn(t *testing.T) {
	started := make(chan context.Context, 1)
	rt := controlruntime.New(controlruntime.RunnerFunc(func(ctx context.Context, _ string, _ controlruntime.RunHost) (string, error) {
		started <- ctx
		<-ctx.Done()
		return "", ctx.Err()
	}), controlruntime.ServiceGroups{})
	defer rt.Close()
	requestCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	response := httptest.NewRecorder()
	New(rt).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"task"}`)).WithContext(requestCtx))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var execution context.Context
	select {
	case execution = <-started:
	case <-time.After(time.Second):
		t.Fatal("accepted run did not start")
	}
	cancel()
	if execution.Err() != nil {
		t.Fatalf("HTTP completion cancelled accepted run: %v", execution.Err())
	}
	run, ok := rt.CurrentRun()
	if !ok {
		t.Fatal("accepted run missing")
	}
	if err := rt.CancelRun(t.Context(), run.ID); err != nil {
		t.Fatal(err)
	}
	if execution.Err() == nil {
		t.Fatal("explicit abort did not cancel HTTP run")
	}
}

func TestInputAdmissionInterruptsStalledBody(t *testing.T) {
	called := false
	rt := &admissionRuntime{fakeRuntime: newFakeRuntime(), start: func(context.Context, controlruntime.Input) (controlruntime.RunID, error) {
		called = true
		return "ghost", nil
	}}
	server := New(rt)
	server.inputAdmissionTimeout = 20 * time.Millisecond
	reader, writer := io.Pipe()
	defer writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/input", reader)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); server.Handler().ServeHTTP(response, request) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		writer.Close()
		<-done
		t.Fatal("stalled body outlived admission deadline")
	}
	if called || response.Code != http.StatusGatewayTimeout {
		t.Fatalf("called=%v status=%d", called, response.Code)
	}
}

func TestInputAdmissionInterruptsNetworkUpload(t *testing.T) {
	rt := &admissionRuntime{fakeRuntime: newFakeRuntime(), start: func(context.Context, controlruntime.Input) (controlruntime.RunID, error) {
		t.Error("stalled input admitted")
		return "ghost", nil
	}}
	adapter := New(rt)
	adapter.inputAdmissionTimeout = 20 * time.Millisecond
	server := httptest.NewServer(adapter.Handler())
	defer server.Close()
	connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := fmt.Fprintf(connection, "POST /input HTTP/1.1\r\nHost: local\r\nContent-Length: 100\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("upload did not time out: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
