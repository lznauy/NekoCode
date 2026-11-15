package telegram

import (
	"errors"
	"fmt"
	"io"
	sharedhttp "nekocode/util/http"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPollHonorsRetryAfterAndDoesNotDelaySuccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{ActiveProfile: "x", Profiles: []BotProfile{{Name: "x", BotToken: "token"}}}); err != nil {
		t.Fatal(err)
	}
	connector := New(nil)
	ctx, generation := connector.base.Start(t.Context())
	observed := make(chan time.Time, 3)
	calls := 0
	client := newAPIClient("token")
	client.client = &http.Client{Transport: pollTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		observed <- time.Now()
		switch calls {
		case 1:
			return &http.Response{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"rate limit","parameters":{"retry_after":1}}`))}, nil
		case 2:
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":[]}`))}, nil
		default:
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
	})}
	done := make(chan struct{})
	go func() { defer close(done); connector.pollLoop(ctx, client, generation) }()
	defer func() { connector.base.Stop(); <-done }()
	times := make([]time.Time, 0, 3)
	for i := 0; i < 3; i++ {
		select {
		case at := <-observed:
			times = append(times, at)
		case <-time.After(2 * time.Second):
			t.Fatal("poll ignored one-second retry_after")
		}
	}
	if elapsed := times[1].Sub(times[0]); elapsed < time.Second || elapsed > 2*time.Second {
		t.Fatalf("retry interval=%v", elapsed)
	}
	if elapsed := times[2].Sub(times[1]); elapsed > 300*time.Millisecond {
		t.Fatalf("successful poll delayed next request by %v", elapsed)
	}
}

func TestPollCancellationInterruptsBackoff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveConfig(Config{ActiveProfile: "x", Profiles: []BotProfile{{Name: "x", BotToken: "token"}}}); err != nil {
		t.Fatal(err)
	}
	connector := New(nil)
	ctx, generation := connector.base.Start(t.Context())
	failed := make(chan struct{})
	client := newAPIClient("token")
	client.client = &http.Client{Transport: pollTestTransport(func(*http.Request) (*http.Response, error) {
		close(failed)
		return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("temporary error"))}, nil
	})}
	done := make(chan struct{})
	go func() { defer close(done); connector.pollLoop(ctx, client, generation) }()
	defer func() { connector.base.Stop(); <-done }()
	<-failed
	time.Sleep(30 * time.Millisecond)
	connector.base.Stop()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("backoff ignored connector cancellation")
	}
}

func TestRetryAfterMetadata(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, body, header string
		want               time.Duration
	}{
		{"body", `{"parameters":{"retry_after":60}}`, "", 60 * time.Second},
		{"header", `not json`, "7", 7 * time.Second},
		{"both", `{"parameters":{"retry_after":60}}`, "120", 120 * time.Second},
		{"http date", `{}`, now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second},
		{"past date", `{}`, now.Add(-time.Minute).Format(http.TimeFormat), pollErrorDelay},
		{"invalid", `{"parameters":{"retry_after":-1}}`, "bad", pollErrorDelay},
		{"fractional", `{"parameters":{"retry_after":0.1}}`, "", pollErrorDelay},
		{"overflow", `{"parameters":{"retry_after":9223372036854775807}}`, "", time.Duration(1<<63 - 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := sharedhttp.NewHTTPError(429, tc.body)
			err := withRetryAfter(cause, []byte(tc.body), tc.header, now)
			if got := pollRetryDelay(fmt.Errorf("poll: %w", err)); got != tc.want {
				t.Fatalf("delay=%v want=%v", got, tc.want)
			}
			var httpErr *sharedhttp.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != 429 {
				t.Fatal("HTTP status lost through retry wrapper")
			}
		})
	}
}

func TestAPIErrorPreservesRetryMetadata(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusOK} {
		client := newAPIClient("token")
		client.client = &http.Client{Transport: pollTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"2"}}, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"rate limit","parameters":{"retry_after":5}}`))}, nil
		})}
		_, err := client.getUpdates(t.Context(), 0, 30)
		if err == nil || pollRetryDelay(err) != 5*time.Second {
			t.Fatalf("status=%d error=%v delay=%v", status, err, pollRetryDelay(err))
		}
	}
}
