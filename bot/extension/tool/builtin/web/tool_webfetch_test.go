package web

import (
	"context"
	"errors"
	"nekocode/bot/extension/tool/runtime/core"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type webRoundTripFunc func(*http.Request) (*http.Response, error)

func mustPageMarkdown(t *testing.T, body []byte, base *url.URL) string {
	t.Helper()
	got, err := pageMarkdown(context.Background(), body, base)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPageMarkdownRejectsExcessiveDepthAndCancellation(t *testing.T) {
	base, _ := url.Parse(testTargetURL)
	deep := strings.Repeat("<div>", 1000) + "text" + strings.Repeat("</div>", 1000)
	if got, err := pageMarkdown(context.Background(), []byte(deep), base); err == nil || got != "" {
		t.Fatalf("deep HTML must not fall back to unchecked conversion: bytes=%d err=%v", len(got), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pageMarkdown(ctx, []byte("<p>text</p>"), base); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled parsing continued: %v", err)
	}
}

func TestPageMarkdownBoundsLinkExpansionAndRendering(t *testing.T) {
	base, _ := url.Parse(testTargetURL)
	for _, input := range []string{
		`<html><head><base href="https://example.com/` + strings.Repeat("a", maxWebURLBytes) + `/"></head><body><a href="x">link</a></body></html>`,
		`<html><head><base href="https://example.com/` + strings.Repeat("a", 8000) + `/"></head><body>` + strings.Repeat(`<a href="x">link</a>`, 1100) + `</body></html>`,
		`<main>` + strings.Repeat("&", 2<<20) + `</main>`,
	} {
		if content, err := pageMarkdown(context.Background(), []byte(input), base); err == nil || content != "" || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("unbounded intermediate expansion: content=%d err=%v", len(content), err)
		}
	}
}

func TestPageMarkdownLargeInlineImageDoesNotRejectDocument(t *testing.T) {
	base, _ := url.Parse(testTargetURL)
	input := `<html><body><img src="data:image/png;base64,` + strings.Repeat("A", 200000) + `"><main><h2>Architecture</h2><p>Real document</p><img src="/diagram.png"></main></body></html>`
	got, err := pageMarkdown(context.Background(), []byte(input), base)
	if err != nil || !strings.Contains(got, "Real document") || !strings.Contains(got, "http://93.184.216.34/diagram.png") {
		t.Fatalf("inline image rejected document: %q %v", got, err)
	}
}

func (f webRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPageMarkdownMainContentAndCitations(t *testing.T) {
	base, _ := url.Parse("https://example.com/docs/page")
	got := mustPageMarkdown(t, []byte(`<html><head><title>metadata</title></head><body><div>menu</div><main><h1>正文</h1><p><a href="../reference?q=1">来源</a></p><img src="/figure.png"><a href="javascript:alert(1)">unsafe</a></main></body></html>`), base)
	for _, want := range []string{"正文", "https://example.com/reference?q=1", "https://example.com/figure.png"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	for _, unwanted := range []string{"metadata", "menu", "javascript:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unexpected %q: %s", unwanted, got)
		}
	}
}

func TestPageMarkdownSelectsDocumentBodyWithoutMain(t *testing.T) {
	base, _ := url.Parse(testTargetURL)
	got := mustPageMarkdown(t, []byte(`<main class="mobile-navigation"><ul><li>global menu</li></ul></main><div class="doc-article-content"><h2>Overview</h2><p>Body</p><h2>Architecture</h2><img src="/diagram.png"></div><div>global footer</div>`), base)
	if !strings.Contains(got, "## Architecture") || strings.Contains(got, "global menu") || strings.Contains(got, "global footer") {
		t.Fatalf("wrong region: %q", got)
	}
}

func TestPageMarkdownKeepsSemanticBodyAroundEmbeddedContent(t *testing.T) {
	base, _ := url.Parse(testTargetURL)
	for _, tag := range []string{"main", "article"} {
		got := mustPageMarkdown(t, []byte("<"+tag+"><h1>Title</h1><p>Intro</p><div class=\"markdown-body\"><p>Embedded README</p></div><h2>Security limits</h2><p>Critical tail</p></"+tag+">"), base)
		if !strings.Contains(got, "Title") || !strings.Contains(got, "Critical tail") {
			t.Fatalf("%s lost substantive siblings: %q", tag, got)
		}
	}
}

func TestWebContentBudgetAndUnmatchedHint(t *testing.T) {
	tool := &WebFetchTool{client: &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return stubResponse(200, "<p>"+strings.Repeat("正文", 4000)+"</p>"), nil
	})}}
	for _, tc := range []struct {
		max  any
		fail bool
	}{{2000.0, false}, {2000, false}, {0.0, true}, {1.5, true}, {30001, true}, {"2000", true}} {
		got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL, "maxChars": tc.max})
		if (err != nil) != tc.fail {
			t.Errorf("maxChars=%v err=%v", tc.max, err)
		}
		if !tc.fail && (len([]rune(got.Text)) > 2000 || !strings.Contains(got.Text, "[Content truncated")) {
			t.Errorf("bad budget: %d", len([]rune(got.Text)))
		}
	}
	got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL, "prompt": "missing_section"})
	if err != nil || !strings.Contains(got.Text, "did not match") {
		t.Fatalf("unmatched hint hidden: %q %v", got.Text, err)
	}
}

func TestPageMarkdownIgnoresHiddenMainAndTemplates(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	for _, hidden := range []string{
		`<main hidden><p>hidden content</p></main>`,
		`<template><main><p>hidden content</p></main></template>`,
		`<div hidden="false"><main><p>hidden content</p></main></div>`,
	} {
		got := mustPageMarkdown(t, []byte(hidden+`<main><p>visible content</p><span hidden>secret</span></main>`), base)
		if !strings.Contains(got, "visible content") || strings.Contains(got, "hidden content") || strings.Contains(got, "secret") {
			t.Errorf("selected hidden content: %q", got)
		}
	}
}

func TestPageMarkdownEmptyMainDoesNotDiscardArticle(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	got := mustPageMarkdown(t, []byte(`<main></main><article><h1>Article</h1><p>Actual content</p></article>`), base)
	if !strings.Contains(got, "Actual content") {
		t.Fatalf("empty main discarded article: %q", got)
	}
}

func TestPageMarkdownHonorsDocumentBase(t *testing.T) {
	base, _ := url.Parse("https://example.com/docs/page")
	got := mustPageMarkdown(t, []byte(`<html><head><base href="/v2/"></head><body><main><a href="reference">Reference</a><a href="https://[bad">Broken</a><a>Anchor</a></main></body></html>`), base)
	if !strings.Contains(got, "https://example.com/v2/reference") || strings.Contains(got, "https://[bad") || strings.Contains(got, "[Anchor](") {
		t.Fatalf("incorrect document links: %s", got)
	}
}

func TestPageMarkdownLinkBoundaries(t *testing.T) {
	base, _ := url.Parse("https://example.com/docs/page")
	for _, tc := range []struct{ html, want, absent string }{
		{`<head><base href="https://cdn.example.com/v1/"><base href="https://ignored.example/"></head><main><a href="ref">Reference</a></main>`, "https://cdn.example.com/v1/ref", "ignored.example"},
		{`<head><base href="javascript:alert(1)"></head><main><a href="ref">Reference</a></main>`, "https://example.com/docs/ref", "javascript:"},
		{`<main><a href="ref_(v2)">Reference</a></main>`, "https://example.com/docs/ref_%28v2%29", "ref_(v2)"},
		{`<main><a href="">Empty</a><a href="javascript:alert(1)">Unsafe</a></main>`, "EmptyUnsafe", "]("},
	} {
		got := mustPageMarkdown(t, []byte(tc.html), base)
		if !strings.Contains(got, tc.want) || strings.Contains(got, tc.absent) {
			t.Errorf("got %q; want %q, absent %q", got, tc.want, tc.absent)
		}
	}
}

func TestExtractionKeepsHeadingAndAdjacentContext(t *testing.T) {
	content := "# API reference\n\nUnrelated introduction\n\n## Authentication\n\nSet the request headers as follows.\n\nAuthorization: Bearer TOKEN\n\nNever log this credential.\n\nUnrelated appendix"
	got := extractRelevant(content, "Authorization")
	for _, want := range []string{"# API reference", "## Authentication", "Set the request headers", "Bearer TOKEN", "Never log"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost context %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Unrelated appendix") {
		t.Errorf("included unrelated tail: %s", got)
	}
}

func TestExtractionSectionBoundariesAndFallback(t *testing.T) {
	content := "## Old section\n\nUnrelated tail\n\n## Authorization\n\nToken instructions\n\n## Next section\n\nOther text"
	got := extractRelevant(content, "Authorization")
	if !strings.Contains(got, "Token instructions") || strings.Contains(got, "Unrelated tail") || strings.Contains(got, "Next section") {
		t.Fatalf("crossed section boundary: %s", got)
	}
	if got := extractRelevant(content, "missing_keyword"); got != content {
		t.Fatal("no-match extraction must preserve original content")
	}
}

func TestExtractionPreservesCompleteCodeBlock(t *testing.T) {
	block := "```python\ndef example():\n\n    before = 1\n\n    TOKEN = 'example'\n\n    after = 2\n\n    return after\n```"
	content := "# Guide\n\nIntroduction\n\n" + block + "\n\nWarning\n\nUnrelated appendix"
	got := extractRelevant(content, "TOKEN")
	if !strings.Contains(got, block) {
		t.Fatalf("extraction broke fenced code block: %s", got)
	}
}

func TestWebContentReportsTruncation(t *testing.T) {
	if got := finalizeContent(" \n\t", ""); got != "Page content is empty" {
		t.Fatalf("whitespace-only page: %q", got)
	}
	got := finalizeContent(strings.Repeat("正文", maxFetchRunes), "")
	if len([]rune(got)) > maxFetchRunes || !strings.Contains(got, "[Content truncated") {
		t.Fatalf("silent or over-budget truncation: %d runes", len([]rune(got)))
	}
}

func TestWebToolsCanceledBeforeURLResolution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tools := []interface {
		Execute(context.Context, map[string]any) (core.ToolOutput, error)
	}{NewWebFetchTool(), NewWebExtractTool()}
	for _, tool := range tools {
		_, err := tool.Execute(ctx, map[string]any{"url": "https://unresolvable-canceled-test.invalid/"})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%T did not preserve cancellation: %v", tool, err)
		}
	}
}

func TestURLValidationCancelsBlockedDNS(t *testing.T) {
	previous := net.DefaultResolver
	entered := make(chan struct{}, 1)
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := validateURLContext(ctx, "https://blocked-dns-test.invalid/")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("DNS validation ignored deadline: %v", err)
	}
	select {
	case <-entered:
	default:
		t.Fatal("DNS stub was never reached")
	}
}

func TestExtractionHandlesVariableAndTildeFences(t *testing.T) {
	for _, block := range []string{
		"````markdown\n# code comment\n\n```\nTOKEN\n```\n\nend\n````",
		"~~~python\n# code comment\n\nbefore\n\nTOKEN\n\nafter\n\nend\n~~~",
	} {
		content := "# Real heading\n\nintro\n\n" + block + "\n\nwarning\n\nappendix"
		got := extractRelevant(content, "TOKEN")
		if !strings.Contains(got, block) || !strings.Contains(got, "# Real heading") {
			t.Fatalf("lost code or real heading: %s", got)
		}
	}
}

func TestExtractFallsBackOnOversizedResponse(t *testing.T) {
	tool, stub := newStubExtractTool(t, func() (*http.Response, error) {
		r := stubResponse(200, strings.Repeat("x", extractMaxBody+1))
		r.Header.Set("Content-Type", "text/markdown")
		return r, nil
	}, stubOK())
	got, err := tool.Execute(context.Background(), map[string]any{"url": testTargetURL})
	if err != nil || !strings.Contains(got.Text, "origin body") || len(stub.requested()) != 2 {
		t.Fatalf("oversized extraction did not fall back: result=%q err=%v requests=%v", got.Text, err, stub.requested())
	}
}

func TestExtractCancellationDoesNotStartFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool, stub := newStubExtractTool(t, func() (*http.Response, error) {
		cancel()
		return nil, ctx.Err()
	}, stubOK())
	_, err := tool.Execute(ctx, map[string]any{"url": testTargetURL})
	if err == nil || len(stub.requested()) != 1 {
		t.Fatalf("canceled extraction started fallback: err=%v requests=%v", err, stub.requested())
	}
}

func TestDirectFetchDecodingAndContentValidation(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body, want string
		fail                          bool
	}{
		{"sniff HTML", "", "<html><body><p>正文</p></body></html>", "正文", false},
		{"GBK", "text/html; charset=gbk", "<p>\xd6\xd0\xce\xc4</p>", "中文", false},
		{"XHTML", "application/xhtml+xml", "<html><body><p>正文</p></body></html>", "正文", false},
		{"JSON", "application/json", `{"ok":true}`, `{"ok":true}`, false},
		{"binary", "application/pdf", "%PDF binary", "", true},
		{"oversize", "text/plain", strings.Repeat("x", (5<<20)+1), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
				r := stubResponse(200, tc.body)
				r.Header.Set("Content-Type", tc.contentType)
				return r, nil
			})}
			got, err := fetchPageDirect(context.Background(), client, testTargetURL)
			if (err != nil) != tc.fail || (!tc.fail && !strings.Contains(got, tc.want)) {
				t.Fatalf("got %q err=%v", got, err)
			}
		})
	}
}

func TestWebRetryBoundaries(t *testing.T) {
	for _, status := range []int{403, 429, 502, 503, 504} {
		calls := 0
		client := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			r := stubResponse(status, "failure")
			r.Header.Set("Retry-After", "0")
			return r, nil
		})}
		_, err := fetchPageDirect(context.Background(), client, testTargetURL)
		want := 2
		if status == 403 {
			want = 1
		}
		if err == nil || calls != want {
			t.Fatalf("status=%d calls=%d err=%v", status, calls, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	client := &http.Client{Timeout: time.Second, Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return stubResponse(503, "busy"), nil
	})}
	if _, err := fetchPageDirect(ctx, client, testTargetURL); err == nil || calls != 1 {
		t.Fatalf("canceled retry: calls=%d err=%v", calls, err)
	}
}

func TestWebFetchTool(t *testing.T) {
	wf := &WebFetchTool{}
	_, err := wf.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing url")
	}
}

func TestDirectFetchReportsJavaScriptChallenge(t *testing.T) {
	client := &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return stubResponse(200, `<script>document.cookie="__tst_status=example";setTimeout("location.reload()",1200);</script>`), nil
	})}
	content, err := fetchPageDirect(context.Background(), client, testTargetURL)
	if err == nil || !strings.Contains(err.Error(), "JavaScript challenge") || !strings.Contains(err.Error(), "web_extract") || content != "" {
		t.Fatalf("challenge must not be reported as an empty successful page: content=%q err=%v", content, err)
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
	if !strings.Contains(got.Render(), "origin body") {
		t.Fatalf("result did not come from the direct fetch: %q", got.Render())
	}
	if requests := stub.requested(); len(requests) != 1 || stub.hit(testExtractorHost) {
		t.Fatalf("web_fetch must make exactly one direct request, got %v", requests)
	}
}
