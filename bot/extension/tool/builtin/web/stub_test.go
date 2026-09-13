package web

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Shared stubs for the web tools. Both hops are stubbed with one RoundTripper so
// the whole Execute path runs offline; targets use a public IP literal because
// validateURL (deliberately) rejects loopback and needs no DNS for literals.

const (
	testExtractorHost = "extractor.test"
	testTargetURL     = "http://93.184.216.34/article"
	testOriginHTML    = `<html><head><title>T</title></head><body><nav>menu</nav><p>origin body</p></body></html>`
	testExtractorBody = "# Extracted\n\nmain content only"
)

type stubTransport struct {
	mu       sync.Mutex
	requests []string
	extract  func() (*http.Response, error)
	origin   func() (*http.Response, error)
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req.URL.String())
	s.mu.Unlock()
	if req.URL.Host == testExtractorHost {
		if s.extract == nil {
			return nil, fmt.Errorf("unexpected extractor request")
		}
		return s.extract()
	}
	if s.origin == nil {
		return nil, fmt.Errorf("unexpected origin request: %s", req.URL)
	}
	return s.origin()
}

func (s *stubTransport) hit(host string) bool {
	return len(s.hits(host)) > 0
}

func (s *stubTransport) hits(host string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []string
	for _, raw := range s.requests {
		if u, err := url.Parse(raw); err == nil && u.Host == host {
			found = append(found, raw)
		}
	}
	return found
}

func (s *stubTransport) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func stubResponse(status int, body string) *http.Response {
	header := http.Header{"Content-Type": {"text/html"}}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d", status), Header: header,
		Body: io.NopCloser(strings.NewReader(body))}
}

func stubOK() func() (*http.Response, error) {
	return func() (*http.Response, error) { return stubResponse(http.StatusOK, testOriginHTML), nil }
}

// newStubExtractTool points defuddleBaseURL at the stub extractor host and wires
// both clients of web_extract to the same stub.
func newStubExtractTool(t *testing.T, extract, origin func() (*http.Response, error)) (*WebExtractTool, *stubTransport) {
	t.Helper()
	previous := defuddleBaseURL
	defuddleBaseURL = "http://" + testExtractorHost + "/"
	t.Cleanup(func() { defuddleBaseURL = previous })

	stub := &stubTransport{extract: extract, origin: origin}
	client := &http.Client{Transport: stub}
	return &WebExtractTool{client: client, extractor: client}, stub
}
