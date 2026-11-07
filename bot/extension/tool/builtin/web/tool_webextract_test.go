package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func stubReaderHTML(html, finalURL string) *http.Response {
	body, _ := json.Marshal(map[string]any{"data": map[string]any{"html": html, "finalUrl": finalURL, "httpStatus": 200}})
	r := stubResponse(200, string(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestWebReaderResolvesLinksAfterOriginRedirect(t *testing.T) {
	tool, _ := newStubExtractTool(t, func() (*http.Response, error) { t.Fatal("valid reader metadata must not fall back"); return nil, nil }, stubOK())
	tool.reader = &http.Client{Transport: webRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") != "application/json" {
			t.Fatal("reader metadata not requested")
		}
		return stubReaderHTML(`<html><body><main><p><a href="install.html">Install</a></p></main></body></html>`, "http://93.184.216.34/docs/v2/index.html"), nil
	})}
	got, err := tool.Execute(context.Background(), map[string]any{"url": "http://93.184.216.34/latest"})
	if err != nil || !strings.Contains(got.Text, "http://93.184.216.34/docs/v2/install.html") {
		t.Fatalf("relative link used pre-redirect URL: %q %v", got.Text, err)
	}
}

func TestWebExtractPrefersExtractor(t *testing.T) {
	tool, stub := newStubExtractTool(t,
		func() (*http.Response, error) { return stubResponse(http.StatusOK, testExtractorBody), nil },
		stubOK(),
	)

	got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Render(), "main content only") {
		t.Fatalf("result did not come from the extractor: %q", got.Render())
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

func TestWebExtractPreservesImageOnlySectionFromReaderHTML(t *testing.T) {
	tool, stub := newStubExtractTool(t, func() (*http.Response, error) {
		t.Fatal("lossy extractor should not run after reader succeeds")
		return nil, nil
	}, stubOK())
	tool.reader = &http.Client{Transport: webRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Respond-With") != "html" {
			t.Fatal("reader must return source HTML")
		}
		if strings.Contains(req.URL.String(), "SECRET") {
			t.Fatal("credential leaked to reader")
		}
		return stubReaderHTML(`<html><head><title>产品介绍</title></head><body><div>global navigation</div><div id="docArticleContent" class="doc-article-content"><h2>产品概述</h2><p>`+strings.Repeat("介绍", 1700)+`</p><h2>平台总体架构</h2><img src="/architecture.png"></div><div>unrelated footer</div></body></html>`, testTargetURL), nil
	})}
	got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL + "?token=SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## 产品概述", "## 平台总体架构", "http://93.184.216.34/architecture.png"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got.Text, "global navigation") || strings.Contains(got.Text, "unrelated footer") || strings.Contains(got.Text, "[Content truncated") {
		t.Fatalf("reader output polluted or prematurely truncated: %.200s", got.Text)
	}
	if len(stub.requested()) != 0 {
		t.Fatalf("unexpected fallback: %v", stub.requested())
	}
	got, err = tool.Execute(context.Background(), map[string]any{"url": testTargetURL, "prompt": "平台总体架构"})
	if err != nil || !strings.Contains(got.Text, "## 平台总体架构") || !strings.Contains(got.Text, "architecture.png") {
		t.Fatalf("targeted section failed: %q %v", got.Text, err)
	}
}

func TestWebExtractReaderFailureDisclosesFallback(t *testing.T) {
	tool, _ := newStubExtractTool(t, func() (*http.Response, error) { return stubResponse(200, "# Partial\n\nFallback body"), nil }, stubOK())
	tool.reader = &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) { return stubResponse(403, "blocked"), nil })}
	got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL})
	if err != nil || !strings.Contains(got.Text, "Fallback body") || !strings.Contains(got.Text, "completeness") {
		t.Fatalf("fallback not disclosed: %q %v", got.Text, err)
	}
	got, err = tool.Execute(context.Background(), map[string]any{"url": testTargetURL, "prompt": "Fallback", "maxChars": 256})
	if err != nil || !strings.Contains(got.Text, "completeness") || len([]rune(got.Text)) > 256 {
		t.Fatalf("hint or budget removed fallback notice: %q %v", got.Text, err)
	}
}

func TestWebExtractReaderRejectsDoubtfulResponses(t *testing.T) {
	for _, response := range []struct {
		status int
		body   string
	}{{200, ""}, {200, "not HTML"}, {200, "<html><body><script>challenge()</script></body></html>"}, {403, "denied"}} {
		tool, _ := newStubExtractTool(t, func() (*http.Response, error) { return stubResponse(200, testExtractorBody), nil }, stubOK())
		tool.reader = &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) { return stubResponse(response.status, response.body), nil })}
		got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL})
		if err != nil || !strings.Contains(got.Text, "main content only") || !strings.Contains(got.Text, "completeness") {
			t.Fatalf("did not fall back: %q %v", got.Text, err)
		}
	}
}

func TestWebExtractReaderHonorsCancellationAndValidation(t *testing.T) {
	tool, stub := newStubExtractTool(t, func() (*http.Response, error) {
		t.Fatal("canceled reader must not start extraction fallback")
		return nil, nil
	}, stubOK())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readerCalls := 0
	tool.reader = &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) { readerCalls++; cancel(); return nil, ctx.Err() })}
	if _, err := tool.Execute(ctx, map[string]any{"url": testTargetURL}); err == nil || readerCalls != 1 || len(stub.requested()) != 0 {
		t.Fatalf("cancellation lost: %v readerCalls=%d fallback=%v", err, readerCalls, stub.requested())
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"url": "http://127.0.0.1/private"}); err == nil || readerCalls != 1 {
		t.Fatalf("private URL reached reader: %v", err)
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
			if !strings.Contains(got.Render(), "origin body") {
				t.Fatalf("fallback did not use the direct fetch: %q", got.Render())
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
