package evals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelClientProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer explicit-test-key" {
			t.Errorf("incorrect request routing or credentials")
		}
		var request struct {
			Model    string           `json:"model"`
			Messages []map[string]any `json:"messages"`
			Tools    []toolDef        `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Model != "test-model" || len(request.Messages) != 1 || request.Messages[0]["tool_call_id"] != "call-1" || len(request.Tools) != 4 {
			t.Error("incorrect request payload")
		}
		if _, ok := request.Messages[0]["IsError"]; ok {
			t.Error("local error flag leaked")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`))
	}))
	defer server.Close()
	client := newModel(modelConfig{Model: "test-model", BaseURL: server.URL + "/v1/", APIKey: "explicit-test-key", MaxTokens: 4096})
	result, err := client.Chat(context.Background(), []message{{Role: "tool", Content: "failed", ToolCallID: "call-1", IsError: true}}, behaviorTools())
	if err != nil || len(result.Choices) != 1 || result.Choices[0].Message.Content != "done" {
		t.Fatalf("response decoding failed: %v", err)
	}
}

func TestModelClientDoesNotFollowRedirectsOrExposeErrorBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect/chat/completions" {
			http.Redirect(w, r, "/leak", http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path == "/leak" {
			t.Error("credential-bearing redirect followed")
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("sensitive-error-body"))
	}))
	defer server.Close()
	for _, path := range []string{"/redirect", "/failure"} {
		client := newModel(modelConfig{BaseURL: server.URL + path, APIKey: "explicit-test-key"})
		_, err := client.Chat(context.Background(), nil, nil)
		if err == nil || strings.Contains(err.Error(), "sensitive-error-body") {
			t.Fatalf("unsafe error response: %v", err)
		}
	}
}
