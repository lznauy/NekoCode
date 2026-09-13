package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestWebExtractPrefersExtractor(t *testing.T) {
	tool, stub := newStubExtractTool(t,
		func() (*http.Response, error) { return stubResponse(http.StatusOK, testExtractorBody), nil },
		stubOK(),
	)

	got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "main content only") {
		t.Fatalf("result did not come from the extractor: %q", got)
	}
	if stub.hit("93.184.216.34") {
		t.Fatalf("origin was fetched even though the extractor succeeded: %v", stub.requested())
	}
	// The target must be handed over in the endpoint's path form.
	want := "http://" + testExtractorHost + "/http://93.184.216.34/article"
	if hits := stub.hits(testExtractorHost); len(hits) != 1 || hits[0] != want {
		t.Fatalf("extractor request = %v, want %s", stub.requested(), want)
	}
}

func TestWebExtractDescriptionLimitsUseToPublicURLs(t *testing.T) {
	description := NewWebExtractTool().Description()
	for _, required := range []string{"public web pages", "do not pass", "credential-bearing URLs", "sent to the defuddle.md"} {
		if !strings.Contains(description, required) {
			t.Errorf("Description() does not disclose %q: %q", required, description)
		}
	}
}

func TestWebExtractFallsBackWhenExtractorFails(t *testing.T) {
	for name, extract := range map[string]func() (*http.Response, error){
		"http 500":      func() (*http.Response, error) { return stubResponse(http.StatusInternalServerError, "boom"), nil },
		"empty body":    func() (*http.Response, error) { return stubResponse(http.StatusOK, "  \n"), nil },
		"html not text": func() (*http.Response, error) { return stubResponse(http.StatusOK, testOriginHTML), nil },
		"transport err": func() (*http.Response, error) { return nil, fmt.Errorf("dial failed") },
	} {
		t.Run(name, func(t *testing.T) {
			tool, stub := newStubExtractTool(t, extract, stubOK())

			got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, "origin body") {
				t.Fatalf("fallback did not use the direct fetch: %q", got)
			}
			if !stub.hit(testExtractorHost) || !stub.hit("93.184.216.34") {
				t.Fatalf("expected both hops to be attempted, got %v", stub.requested())
			}
		})
	}
}

// A private target must be refused locally and never handed to the extractor.
func TestWebExtractRejectsPrivateTargetBeforeExtractor(t *testing.T) {
	tool, stub := newStubExtractTool(t,
		func() (*http.Response, error) { return stubResponse(http.StatusOK, testExtractorBody), nil },
		stubOK(),
	)

	for _, rawURL := range []string{"http://127.0.0.1/private", "http://10.0.0.1/", "file:///etc/passwd"} {
		if _, err := tool.Execute(context.Background(), map[string]any{"url": rawURL}); err == nil {
			t.Fatalf("%s was allowed", rawURL)
		}
	}
	if got := stub.requested(); len(got) != 0 {
		t.Fatalf("refused targets must not leave the process, got requests %v", got)
	}
}

// The extractor hop may use the environment proxy (its destination is the fixed
// defuddleBaseURL host), while the local fallback must not: that one carries the
// user-supplied URL and keeps the SSRF boundary.
func TestWebExtractClientProxyPolicy(t *testing.T) {
	tool := NewWebExtractTool()

	extractorTransport, ok := tool.extractor.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("extractor transport type = %T, want *http.Transport", tool.extractor.Transport)
	}
	if extractorTransport.Proxy == nil {
		t.Fatal("extractor hop should honor the environment proxy")
	}

	fallbackTransport, ok := tool.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("fallback transport type = %T, want *http.Transport", tool.client.Transport)
	}
	if fallbackTransport.Proxy != nil {
		t.Fatal("fallback fetch must connect directly so destination-IP SSRF checks cannot be bypassed by a proxy")
	}
}

func TestHardenedDialerAllowsOnlyConfiguredProxy(t *testing.T) {
	// A loopback address is what a local proxy typically listens on.
	proxyAddress := "127.0.0.1:7897"

	// The configured proxy address skips the private-IP rejection: whether the
	// connection then succeeds is a transport concern, not our policy check.
	_, err := hardenedDialer(true, proxyAddress)(context.Background(), "tcp", proxyAddress)
	if err != nil && strings.Contains(err.Error(), "private network access denied") {
		t.Fatalf("configured proxy address was refused by the SSRF check: %v", err)
	}

	// Every other destination still goes through the check, with or without the
	// proxy exception...
	for _, allowProxy := range []bool{false, true} {
		_, err := hardenedDialer(allowProxy, proxyAddress)(context.Background(), "tcp", "127.0.0.1:9")
		if err == nil || !strings.Contains(err.Error(), "private network access denied") {
			t.Fatalf("loopback destination was not refused (allowProxy=%v): %v", allowProxy, err)
		}
	}
	// ...and with no proxy configured the exception cannot trigger at all.
	_, err = hardenedDialer(true, "")(context.Background(), "tcp", proxyAddress)
	if err == nil || !strings.Contains(err.Error(), "private network access denied") {
		t.Fatalf("loopback destination was not refused without a proxy configured: %v", err)
	}
}

func TestDefuddleTarget(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"https://example.com/a/b", "https://example.com/a/b"},
		{"http://example.com/a/b", "http://example.com/a/b"},
		{"https://example.com/a?b=1&c=2", "https://example.com/a?b=1&c=2"},
		{"https://example.com", "https://example.com"},
		{"https://example.com/a%20b", "https://example.com/a%20b"},
		// Credential-bearing parameters are stripped before the URL leaves
		// the process; the local fallback alone sees the full query.
		{"https://example.com/a?b=1&token=SECRET", "https://example.com/a?b=1"},
		{"https://example.com/a?token=SECRET", "https://example.com/a"},
		{"https://example.com/a?Signature=abc&X-Amz-Signature=xyz", "https://example.com/a"},
	}
	for _, c := range cases {
		got, err := defuddleTarget(c.raw)
		if err != nil {
			t.Fatalf("defuddleTarget(%q): %v", c.raw, err)
		}
		if got != c.want {
			t.Errorf("defuddleTarget(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
	if _, err := defuddleTarget("not-a-url"); err == nil {
		t.Error("hostless input should be rejected")
	}
	// Paths that could disturb the endpoint's own routing are refused.
	for _, raw := range []string{
		"https://example.com/a%2fb",
		"https://example.com//a",
		"https://example.com/a%2e%2eb",
	} {
		if _, err := defuddleTarget(raw); err == nil {
			t.Errorf("defuddleTarget(%q) should be rejected", raw)
		}
	}
}
