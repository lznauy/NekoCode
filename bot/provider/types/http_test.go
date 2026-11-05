package types

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const streamReturnTimeout = 2 * time.Second

type blockingStreamBody struct {
	reader    *strings.Reader
	closed    chan struct{}
	closeOnce sync.Once
}

func (b *blockingStreamBody) Read(p []byte) (int, error) {
	if b.reader.Len() > 0 {
		return b.reader.Read(p)
	}
	<-b.closed
	return 0, io.EOF
}

func (b *blockingStreamBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func TestStreamSSEStopsAtDoneWithoutWaitingForEOF(t *testing.T) {
	body := &blockingStreamBody{
		reader: strings.NewReader("data: payload\n\ndata: [DONE]\n\n"),
		closed: make(chan struct{}),
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	errCh := make(chan error, 1)
	returned := make(chan struct{})
	parsed := 0

	go func() {
		StreamSSE(context.Background(), resp, make(chan StreamToken), errCh,
			func(data string, _ chan<- StreamToken) error {
				parsed++
				return nil
			})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(streamReturnTimeout):
		_ = body.Close()
		t.Fatal("StreamSSE waited for EOF after receiving [DONE]")
	}
	if parsed != 1 {
		t.Fatalf("parsed events = %d, want 1", parsed)
	}
	select {
	case err := <-errCh:
		t.Fatalf("unexpected stream error: %v", err)
	default:
	}
}

func TestStreamSSEStopsOnProviderTerminalEvent(t *testing.T) {
	body := &blockingStreamBody{
		reader: strings.NewReader("data: message-stop\n\n"),
		closed: make(chan struct{}),
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	returned := make(chan struct{})

	go func() {
		StreamSSE(context.Background(), resp, make(chan StreamToken), make(chan error, 1),
			func(string, chan<- StreamToken) error { return ErrStreamDone })
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(streamReturnTimeout):
		_ = body.Close()
		t.Fatal("StreamSSE waited for EOF after a provider terminal event")
	}
}

func TestStreamSSEDispatchesDataEvents(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		want          []string
	}{
		{"multiline JSON", "data: {\"choices\":\r\ndata: []}\r\n\r\ndata: [DONE]\r\n\r\n", []string{"{\"choices\":\n[]}"}},
		{"fields and empty data", ": comment\nevent: message\nid: 1\ndata\ndata:  tail\nretry: 10\n\ndata: [DONE]\n\n", []string{"\n tail"}},
		{"BOM and CR lines", "\ufeffdata: hello\rdata: world\r\rdata: [DONE]\r\r", []string{"hello\nworld"}},
		{"empty events", "\nevent: message\n\ndata:\n\ndata: [DONE]\n\n", []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			errs := make(chan error, 1)
			StreamSSE(context.Background(), &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.payload))}, make(chan StreamToken), errs, func(data string, _ chan<- StreamToken) error { got = append(got, data); return nil })
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("events = %#v, want %#v", got, tc.want)
			}
			select {
			case err := <-errs:
				t.Fatal(err)
			default:
			}
		})
	}
}

func TestStreamSSEDiscardsUnterminatedEvent(t *testing.T) {
	for _, payload := range []string{"data: partial", "data: [DONE]\n"} {
		errs := make(chan error, 1)
		StreamSSE(context.Background(), &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload))}, make(chan StreamToken), errs, func(string, chan<- StreamToken) error { t.Error("dispatched incomplete event"); return nil })
		select {
		case err := <-errs:
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatal(err)
			}
		default:
			t.Fatal("missing EOF error")
		}
	}
}

func TestStreamSSEBoundsMultilineEvent(t *testing.T) {
	errs := make(chan error, 1)
	payload := strings.Repeat("data: "+strings.Repeat("x", 1024)+"\n", 1025) + "\n"
	StreamSSE(context.Background(), &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload))}, make(chan StreamToken), errs, func(string, chan<- StreamToken) error { t.Error("dispatched oversized event"); return nil })
	select {
	case err := <-errs:
		if err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("expected size error, got %v", err)
		}
	default:
		t.Fatal("missing size error")
	}
}
