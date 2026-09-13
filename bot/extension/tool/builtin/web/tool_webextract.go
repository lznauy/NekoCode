package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/logger"
	utilhttp "nekocode/util/http"
)

// defuddleBaseURL is the hosted Defuddle endpoint: appending a target's host
// and path returns that page's main content as Markdown. The page itself is
// fetched by that service, so the (redacted) target URL does leave the
// process; local validation still refuses private or loopback targets before
// the request is made.
var defuddleBaseURL = "https://defuddle.md/"

const (
	// extractTimeout bounds the third-party hop so the local fallback still has
	// room inside the request budget.
	extractTimeout = 12 * time.Second
	// extractMaxBody caps the extracted Markdown we are willing to read.
	extractMaxBody = 1 << 20
)

type WebExtractTool struct {
	toolutil.SafeReadOnlyTool
	// client fetches user-supplied URLs when the extractor is unavailable; it is
	// the same hardened, proxy-free client web_fetch uses, so the fallback keeps
	// the SSRF boundary intact.
	client *http.Client
	// extractor talks to the constant defuddleBaseURL host.
	extractor *http.Client
}

func NewWebExtractTool() *WebExtractTool {
	return &WebExtractTool{client: newDirectClient(), extractor: newExtractorClient()}
}

// newExtractorClient builds the client for the extractor hop. Its destination is
// the fixed defuddleBaseURL host rather than user input (the user's URL is only
// a locally validated path component), so honoring the environment proxy here
// does not move the SSRF check onto the proxy address the way it would for the
// user-supplied destinations newDirectClient handles.
func newExtractorClient() *http.Client {
	transport := utilhttp.NewSharedTransport() // Proxy: ProxyFromEnvironment
	transport.DialContext = hardenedDialer(true, proxyHostPort(defuddleBaseURL))

	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	client.CheckRedirect = checkRedirect
	return client
}

// proxyHostPort returns the "host:port" the environment configures for
// rawURL's scheme, or "" when requests go direct.
func proxyHostPort(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	proxyURL, err := http.ProxyFromEnvironment(&http.Request{URL: u})
	if err != nil || proxyURL == nil {
		return ""
	}
	if port := proxyURL.Port(); port != "" {
		return proxyURL.Host
	}
	if proxyURL.Scheme == "https" {
		return net.JoinHostPort(proxyURL.Hostname(), "443")
	}
	return net.JoinHostPort(proxyURL.Hostname(), "80")
}

func (t *WebExtractTool) Name() string { return "web_extract" }

func (t *WebExtractTool) Description() string {
	return "Extract a public web page's main content as Markdown, dropping navigation, ads, and boilerplate. Use only for public web pages: do not pass authenticated, private, signed, one-time, or credential-bearing URLs. The target URL is sent to the defuddle.md extraction service; if that fails the page is fetched and converted locally instead. Private, intranet, and loopback addresses are rejected either way. When quoting, cite the source URL and keep quotes ≤125 characters."
}

func (t *WebExtractTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "url", Type: "string", Required: true, Description: "Web page URL to extract"},
		{Name: "prompt", Type: "string", Required: false, Description: "Content extraction hint, e.g. 'extract API parameters'"},
	}
}

func (t *WebExtractTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	rawURL, err := toolutil.RequireStringArg(args, "url")
	if err != nil {
		return "", err
	}

	// Validate before anything else: a private, loopback, or non-http(s) target
	// must be refused locally rather than handed to the extractor, and the answer
	// must not depend on how that third party treats such a URL.
	if err := validateURL(rawURL); err != nil {
		return "", fmt.Errorf("URL validation failed: %w", err)
	}

	prompt := toolutil.OptStringArg(args, "prompt", "")

	content, err := t.extract(ctx, rawURL)
	if err != nil {
		// The error text of a transport failure wraps the third-party request
		// URL, which carries the user's query: log only the underlying cause.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		logger.Log("web_extract: extractor unavailable (%v); falling back to direct fetch", err)
		content, err = fetchPageDirect(ctx, t.client, rawURL)
		if err != nil {
			return "", err
		}
	}
	return finalizeContent(content, prompt), nil
}

// extract asks the hosted extractor to convert rawURL into Markdown. Any
// doubtful response (non-200, empty, or an HTML page) is an error so the caller
// falls back to the local fetch; rawURL was already validated locally.
func (t *WebExtractTool) extract(ctx context.Context, rawURL string) (string, error) {
	if t.extractor == nil {
		return "", fmt.Errorf("extractor not configured")
	}
	target, err := defuddleTarget(rawURL)
	if err != nil {
		return "", err
	}

	hopCtx, cancel := context.WithTimeout(ctx, extractTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(hopCtx, http.MethodGet, defuddleBaseURL+target, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build extractor request: %w", err)
	}
	req.Header.Set("User-Agent", "NekoCode/1.0")
	req.Header.Set("Accept", "text/markdown,text/plain,*/*")

	resp, err := t.extractor.Do(req)
	if err != nil {
		return "", fmt.Errorf("extractor request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("extractor HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, extractMaxBody))
	if err != nil {
		return "", fmt.Errorf("failed to read extractor response: %w", err)
	}
	content := strings.TrimSpace(string(body))
	if content == "" {
		return "", fmt.Errorf("extractor returned no content")
	}
	if looksLikeHTML(content) {
		return "", fmt.Errorf("extractor returned HTML instead of markdown")
	}
	return content, nil
}

// defuddleTarget renders rawURL in the endpoint's path form (the target's host
// and path appended to the base). Credential-bearing query parameters are
// stripped so signed URLs and API tokens are not handed to the third party;
// the local fallback alone talks to the origin with the full query. Paths with
// dot segments, duplicate slashes, or encoded slashes are rejected: they can
// disturb the endpoint's own routing.
func defuddleTarget(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing hostname")
	}
	path := u.EscapedPath()
	lower := strings.ToLower(path)
	if strings.Contains(path, "..") || strings.Contains(path, "//") ||
		strings.Contains(lower, "%2f") || strings.Contains(lower, "%2e") {
		return "", fmt.Errorf("target path contains unexpected segments")
	}
	target := u.Scheme + "://" + u.Host + path
	if u.RawQuery != "" {
		if filtered := redactQuery(u.RawQuery); filtered != "" {
			target += "?" + filtered
		}
	}
	return target, nil
}

// sensitiveQueryKeys are parameter names that carry credentials. They are
// stripped before the URL leaves the process toward the extractor.
var sensitiveQueryKeys = map[string]bool{
	"token": true, "access_token": true, "auth": true, "apikey": true, "api_key": true,
	"key": true, "password": true, "passwd": true, "secret": true,
	"signature": true, "sig": true, "session": true, "sessionid": true,
	"credential":      true,
	"x-amz-signature": true, "x-amz-credential": true, "x-amz-security-token": true,
	"x-goog-signature": true, "x-azure-signature": true, "x-ms-signature": true,
}

// redactQuery drops credential-bearing parameters and re-encodes the rest. An
// undecodable query is dropped entirely rather than forwarded verbatim.
func redactQuery(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return ""
	}
	kept := url.Values{}
	for key, vals := range values {
		if sensitiveQueryKeys[strings.ToLower(key)] {
			continue
		}
		kept[key] = vals
	}
	return kept.Encode()
}

// looksLikeHTML reports whether a payload is a page rather than converted text,
// which is how a failed extraction tends to come back. Converted Markdown can
// still contain HTML fragments, so only a payload that starts with markup is
// treated as a page.
func looksLikeHTML(content string) bool {
	head := content
	if len(head) > 512 {
		head = head[:512]
	}
	head = strings.ToLower(strings.TrimSpace(head))
	if !strings.HasPrefix(head, "<") {
		return false
	}
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") ||
		strings.Contains(head, "<head>") || strings.Contains(head, "<body")
}
