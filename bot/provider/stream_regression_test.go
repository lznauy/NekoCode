package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"nekocode/bot/provider/anthropic"
	"nekocode/bot/provider/openai"
	"nekocode/bot/provider/types"
	utilhttp "nekocode/util/http"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamCompletionAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, payload string
		wantErr, retry          bool
		status                  int
		usage                   bool
		content                 string
	}{
		{name: "openai multiline", payload: "{\"choices\":\ndata: [{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]", content: "hello"},
		{name: "anthropic multiline", protocol: "anthropic", payload: "{\"type\":\"content_block_delta\",\ndata: \"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\ndata: {\"type\":\"message_stop\"}", content: "hello"},
		{name: "openai error", payload: `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"busy"}}`, wantErr: true, retry: true, status: 429},
		{name: "anthropic error", protocol: "anthropic", payload: `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`, wantErr: true, retry: true, status: 529},
		{name: "openai auth error", payload: `{"error":{"type":"authentication_error","message":"invalid"}}`, wantErr: true, status: 401},
		{name: "openai truncated", payload: `{"choices":[{"delta":{"content":"partial"}}]}`, wantErr: true, retry: true},
		{name: "anthropic truncated", protocol: "anthropic", payload: `{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}`, wantErr: true, retry: true},
		{name: "tool truncated", payload: `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call","function":{"name":"x","arguments":"{"}}]}}]}`, wantErr: true, retry: true},
		{name: "openai finish usage", payload: "{\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"completion_tokens\":7}}", usage: true},
		{name: "openai done", payload: "[DONE]"},
		{name: "anthropic stop", protocol: "anthropic", payload: `{"type":"message_stop"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "data: "+tc.payload+"\n\n") }))
			defer s.Close()
			var tokens <-chan types.StreamToken
			var errs <-chan error
			if tc.protocol == "anthropic" {
				tokens, errs = anthropic.New("k", s.URL, "m").ChatStream(context.Background(), nil, nil)
			} else {
				tokens, errs = openai.New("k", s.URL, "m").ChatStream(context.Background(), nil, nil)
			}
			usage := false
			var content string
			for token := range tokens {
				content += token.Content
				if token.Usage != nil && token.Usage.CompletionTokens == 7 {
					usage = true
				}
			}
			err := <-errs
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr && IsRetryable(err) != tc.retry {
				t.Errorf("retryable=%v error=%v", IsRetryable(err), err)
			}
			if tc.status != 0 {
				var he *utilhttp.HTTPError
				if !errors.As(err, &he) || he.StatusCode != tc.status {
					t.Errorf("typed error=%v, want status %d", err, tc.status)
				}
			}
			if tc.content != "" && content != tc.content {
				t.Errorf("content = %q, want %q", content, tc.content)
			}
			if tc.usage && !usage {
				t.Error("lost final usage")
			}
		})
	}
}

func TestStreamCancellationUnblocksProducer(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, consumeMeta := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/meta=%v", protocol, consumeMeta), func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					payload := `{"choices":[{"delta":{"content":"text"}}]}`
					if protocol == "anthropic" {
						payload = `{"type":"content_block_delta","delta":{"type":"text_delta","text":"text"}}`
					}
					io.WriteString(w, "data: "+payload+"\n\n")
				}))
				defer s.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var tokens <-chan types.StreamToken
				var errs <-chan error
				if protocol == "anthropic" {
					tokens, errs = anthropic.New("k", s.URL, "m").ChatStream(ctx, nil, nil)
				} else {
					tokens, errs = openai.New("k", s.URL, "m").ChatStream(ctx, nil, nil)
				}
				if consumeMeta {
					<-tokens
				}
				cancel()
				select {
				case err := <-errs:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error=%v", err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("producer stranded after cancellation")
				}
				for range tokens {
				}
			})
		}
	}
}
