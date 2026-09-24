package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEndpointTransportInjectsConfiguredHeaders(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := endpointTransport{base: http.DefaultTransport, headers: map[string]string{
		"Authorization": "Bearer static-token",
		"X-Custom":      "value",
	}}
	client := &http.Client{Transport: transport}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if got.Get("Authorization") != "Bearer static-token" {
		t.Fatalf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("X-Custom") != "value" {
		t.Fatalf("X-Custom = %q", got.Get("X-Custom"))
	}
}

func TestEndpointTransportKeepsExistingHeader(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := endpointTransport{base: http.DefaultTransport, headers: map[string]string{
		"Authorization": "Bearer static-token",
	}}
	client := &http.Client{Transport: transport}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// The OAuth handler sets its own Authorization header; the static one
	// must not override it.
	req.Header.Set("Authorization", "Bearer oauth-token")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if got.Get("Authorization") != "Bearer oauth-token" {
		t.Fatalf("Authorization = %q", got.Get("Authorization"))
	}
}
