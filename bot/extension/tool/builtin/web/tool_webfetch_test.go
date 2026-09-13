package web

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestWebFetchTool(t *testing.T) {
	wf := &WebFetchTool{}
	_, err := wf.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing url")
	}
}

func TestValidateURLRejectsPrivateNetworks(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://[::1]/",
		"http://100.64.0.1/",
	} {
		if err := validateURL(rawURL); err == nil {
			t.Errorf("validateURL(%q) allowed a private address", rawURL)
		}
	}
	if isPrivateIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address was classified as private")
	}
}

func TestWebFetchRedirectRejectsPrivateTarget(t *testing.T) {
	client := NewWebFetchTool().client
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/private", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("redirect to private network was allowed")
	}
}

func TestWebFetchIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:7897")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")

	client := NewWebFetchTool().client
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("web_fetch must connect directly so destination-IP SSRF checks cannot be bypassed by a proxy")
	}
}

// web_fetch carries the user-supplied URL and stays third-party free: the
// extraction service belongs to web_extract only.
func TestWebFetchNeverCallsExtractor(t *testing.T) {
	previous := defuddleBaseURL
	defuddleBaseURL = "http://" + testExtractorHost + "/"
	t.Cleanup(func() { defuddleBaseURL = previous })

	stub := &stubTransport{origin: stubOK()}
	tool := &WebFetchTool{client: &http.Client{Transport: stub}}

	got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "origin body") {
		t.Fatalf("result did not come from the direct fetch: %q", got)
	}
	if requests := stub.requested(); len(requests) != 1 || stub.hit(testExtractorHost) {
		t.Fatalf("web_fetch must make exactly one direct request, got %v", requests)
	}
}
