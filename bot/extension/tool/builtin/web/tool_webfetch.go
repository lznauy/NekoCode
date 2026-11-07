package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	utilhttp "nekocode/util/http"
	"nekocode/util/text"
)

// maxFetchRunes is the content budget a fetched page is trimmed to.
const maxFetchRunes = 12000
const maxWebURLBytes = 16 << 10

func webContentLimit(args map[string]any) (int, error) {
	raw, present := args["maxChars"]
	if !present {
		return maxFetchRunes, nil
	}
	var value float64
	switch v := raw.(type) {
	case int:
		value = float64(v)
	case float64:
		value = v
	default:
		return 0, fmt.Errorf("maxChars must be an integer between 256 and 30000")
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 256 || value > 30000 || math.Trunc(value) != value {
		return 0, fmt.Errorf("maxChars must be an integer between 256 and 30000")
	}
	return int(value), nil
}

func webContentLimitParameter() core.Parameter {
	return core.Parameter{Name: "maxChars", Type: "number", Required: false, Description: "Maximum returned characters, default 12000, integer from 256 to 30000. A truncation notice is included when content exceeds this budget. Use prompt to focus on a specific section."}
}

type WebFetchTool struct {
	toolutil.SafeReadOnlyTool
	client *http.Client
}

func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{client: newDirectClient()}
}

// newDirectClient builds the client used for user-supplied URLs: it deliberately
// ignores process proxy variables, because the custom DialContext validates and
// pins the actual destination IP, and an HTTP proxy would make it validate only
// the proxy address, weakening the SSRF boundary. The extractor hop of
// web_extract uses a different client for a constant destination.
func newDirectClient() *http.Client {
	transport := utilhttp.NewSharedTransport()
	transport.Proxy = nil
	transport.DialContext = hardenedDialer(false, "")

	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	client.CheckRedirect = checkRedirect
	return client
}

// checkRedirect keeps every hop, redirect targets included, inside the public
// network.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("too many redirects")
	}
	return validateURLContext(req.Context(), req.URL.String())
}

// hardenedDialer resolves and pins the destination IP, refusing private
// addresses. When allowProxy is set, the address of the proxy configured for
// rawURL is dialed as-is: that address is the user's own egress, and the proxy
// resolves the real destination on our behalf.
func hardenedDialer(allowProxy bool, proxyHostPort string) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if allowProxy && proxyHostPort != "" && address == proxyHostPort {
			return dialer.DialContext(ctx, network, address)
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid destination: %w", err)
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve destination: %w", err)
		}
		var lastErr error
		for _, ip := range ips {
			if isPrivateIP(ip) {
				return nil, fmt.Errorf("private network access denied")
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("destination has no public IP address")
	}
}

func (t *WebFetchTool) Name() string { return "web_fetch" }

func (t *WebFetchTool) Description() string {
	return "Fetch a public web page as text; private, intranet, and loopback addresses are rejected. Filtered or truncated results preserve the full retrieved content as an actor-owned artifact; use read on that file and its section line numbers to retrieve omitted details. Retrieved content is not proof of source completeness. When quoting, cite the source URL and keep quotes ≤125 characters."
}

func (t *WebFetchTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "url", Type: "string", Required: true, Description: "Web page URL to fetch"},
		{Name: "prompt", Type: "string", Required: false, Description: "Content extraction hint, e.g. 'extract API parameters'"},
		webContentLimitParameter(),
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	limit, err := webContentLimit(args)
	if err != nil {
		return core.ToolOutput{}, err
	}
	rawURL, err := toolutil.RequireStringArg(args, "url")
	if err != nil {
		return core.ToolOutput{}, err
	}

	if err := validateURLContext(ctx, rawURL); err != nil {
		return core.ToolOutput{}, fmt.Errorf("URL validation failed: %w", err)
	}

	prompt := toolutil.OptStringArg(args, "prompt", "")

	content, err := fetchPageDirect(ctx, t.client, rawURL)
	if err != nil {
		return core.ToolOutput{}, err
	}
	return webResult(ctx, rawURL, "direct-http", content, prompt, limit, false)
}

// fetchPageDirect fetches rawURL straight from the origin and converts HTML to
// Markdown. Shared by web_fetch and by the local fallback of web_extract.
func fetchPageDirect(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("User-Agent", "NekoCode/1.0")
	req.Header.Set("Accept", "text/html,text/plain,*/*")

	resp, err := doWebRequest(client, req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := readWebBody(resp.Body, 5<<20)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	isHTML := strings.EqualFold(mediaType, "text/html") || strings.EqualFold(mediaType, "application/xhtml+xml")
	if isHTML || strings.HasPrefix(strings.ToLower(mediaType), "text/") {
		decoded, err := charset.NewReader(strings.NewReader(string(body)), contentType)
		if err != nil {
			return "", fmt.Errorf("decode page charset: %w", err)
		}
		body, err = readWebBody(decoded, 15<<20)
		if err != nil {
			return "", err
		}
	}
	if isHTML {
		base := req.URL
		if resp.Request != nil && resp.Request.URL != nil {
			base = resp.Request.URL
		}
		content, err := pageMarkdown(ctx, body, base)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(content) == "" && (bytes.Contains(body, []byte("__tst_status")) || bytes.Contains(body, []byte("EO_Bot_Ssid"))) {
			return "", fmt.Errorf("origin returned a JavaScript challenge instead of page content (HTTP %d); try web_extract for this public URL", resp.StatusCode)
		}
		return content, nil
	}
	if mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") && !strings.HasPrefix(mediaType, "text/") {
		return "", fmt.Errorf("unsupported page content type %q; expected HTML or text", mediaType)
	}
	return string(body), nil
}

// Resolve citations against the final URL, and prefer a declared main region
// so menus and metadata cannot consume the short page-content budget.
func pageMarkdown(ctx context.Context, body []byte, base *url.URL) (string, error) {
	doc, err := html.Parse(&webContextReader{ctx: ctx, Reader: bytes.NewReader(body)})
	if err != nil {
		return "", fmt.Errorf("parse page HTML: %w", err)
	}
	// Templates and elements with a hidden attribute do not contribute rendered
	// content. Remove them before selecting main or resolving a document base.
	var pruneHidden func(*html.Node)
	pruneHidden = func(n *html.Node) {
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			hidden := child.Type == html.ElementNode && child.Data == "template"
			if child.Type == html.ElementNode {
				for _, attr := range child.Attr {
					if attr.Key == "hidden" {
						hidden = true
						break
					}
				}
			}
			if hidden {
				n.RemoveChild(child)
			} else {
				pruneHidden(child)
			}
			child = next
		}
	}
	pruneHidden(doc)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var main *html.Node
	var article *html.Node
	var documentBody *html.Node
	// Only the first base element with an href controls relative references.
	var findBase func(*html.Node) bool
	var baseErr error
	findBase = func(n *html.Node) bool {
		if err := ctx.Err(); err != nil {
			baseErr = err
			return true
		}
		if n.Type == html.ElementNode && n.Data == "base" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					if len(attr.Val) > maxWebURLBytes {
						baseErr = fmt.Errorf("HTML base URL exceeds %d byte limit", maxWebURLBytes)
						return true
					}
					if ref, err := url.Parse(attr.Val); err == nil {
						resolved := base.ResolveReference(ref)
						if len(resolved.String()) > maxWebURLBytes {
							baseErr = fmt.Errorf("HTML base URL exceeds %d byte limit", maxWebURLBytes)
							return true
						}
						if resolved.Scheme == "http" || resolved.Scheme == "https" {
							base = resolved
						}
					}
					return true
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if findBase(child) {
				return true
			}
		}
		return false
	}
	findBase(doc)
	if baseErr != nil {
		return "", baseErr
	}
	var expandedURLBytes int
	var visit func(*html.Node) error
	visit = func(n *html.Node) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if n.Type == html.ElementNode {
			if n.Data == "main" && main == nil {
				main = n
			}
			if n.Data == "article" && article == nil {
				article = n
			}
			for _, attr := range n.Attr {
				if attr.Key == "role" && attr.Val == "main" && main == nil {
					main = n
				}
				if documentBody == nil && ((attr.Key == "itemprop" && attr.Val == "articleBody") || (attr.Key == "class" && isDocumentBodyClass(attr.Val))) {
					documentBody = n
				}
			}
			for i := range n.Attr {
				attr := &n.Attr[i]
				if (n.Data == "a" && attr.Key == "href") || (n.Data == "img" && attr.Key == "src") {
					raw := strings.TrimSpace(attr.Val)
					attr.Val = ""
					// Inline images and non-web schemes were never emitted as
					// source links. Discard them before URL-size checks so a large
					// data URI cannot make an otherwise readable page fail.
					if colon := strings.IndexByte(raw, ':'); colon >= 0 && !strings.ContainsAny(raw[:colon], "/?#") {
						scheme := strings.ToLower(raw[:colon])
						if scheme != "http" && scheme != "https" {
							continue
						}
					}
					if len(raw) > maxWebURLBytes {
						return fmt.Errorf("HTML link URL exceeds %d byte limit", maxWebURLBytes)
					}
					if ref, err := url.Parse(raw); err == nil && strings.TrimSpace(raw) != "" {
						resolved := base.ResolveReference(ref)
						if resolved.Scheme == "https" || resolved.Scheme == "http" {
							absolute := strings.NewReplacer("(", "%28", ")", "%29").Replace(resolved.String())
							if len(absolute) > maxWebURLBytes {
								return fmt.Errorf("HTML resolved URL exceeds %d byte limit", maxWebURLBytes)
							}
							expandedURLBytes += len(absolute)
							if expandedURLBytes > maxHTMLMarkdownBytes {
								return fmt.Errorf("HTML link expansion exceeds %d byte limit", maxHTMLMarkdownBytes)
							}
							attr.Val = absolute
						}
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(doc); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// A nested content class must not discard its enclosing semantic article's
	// title and sibling sections. An unrelated mobile main remains lower priority.
	var enclosingBody *html.Node
	if documentBody != nil {
		for p := documentBody.Parent; p != nil; p = p.Parent {
			semantic := p.Type == html.ElementNode && (p.Data == "article" || p.Data == "main")
			for _, attr := range p.Attr {
				if attr.Key == "role" && attr.Val == "main" {
					semantic = true
				}
			}
			if semantic {
				enclosingBody = p
				break
			}
		}
	}
	// Explicit document-body containers are more specific than main: some
	// sites use main for a mobile navigation panel outside the document.
	for _, region := range []*html.Node{enclosingBody, documentBody, main, article} {
		if region == nil {
			continue
		}
		var rendered bytes.Buffer
		if err := html.Render(&webRenderWriter{ctx: ctx, buffer: &rendered}, region); err == nil {
			content, err := html2md(ctx, rendered.String())
			if err != nil {
				return "", err
			}
			if content != "" {
				return content, nil
			}
		} else {
			return "", fmt.Errorf("render page HTML: %w", err)
		}
	}
	var rendered bytes.Buffer
	if err := html.Render(&webRenderWriter{ctx: ctx, buffer: &rendered}, doc); err != nil {
		return "", fmt.Errorf("render page HTML: %w", err)
	}
	return html2md(ctx, rendered.String())
}

type webRenderWriter struct {
	ctx    context.Context
	buffer *bytes.Buffer
}

func (w *webRenderWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > maxHTMLMarkdownBytes-w.buffer.Len() {
		return 0, fmt.Errorf("HTML rendering exceeds %d byte limit", maxHTMLMarkdownBytes)
	}
	return w.buffer.Write(p)
}

type webContextReader struct {
	ctx context.Context
	io.Reader
}

func (r *webContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// Common semantic content classes, independent of the source hostname.
func isDocumentBodyClass(value string) bool {
	for _, class := range strings.Fields(value) {
		switch class {
		case "doc-article-content", "article-content", "article-body", "document-content", "markdown-body", "post-content", "entry-content":
			return true
		}
	}
	return false
}

// Detect truncation instead of presenting an incomplete document as complete.
func readWebBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("web response exceeds %d byte limit", limit)
	}
	return body, nil
}

// Retry only transient HTTP responses, once. Transport/validation errors are
// left intact, and the original client timeout bounds both attempts together.
func doWebRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if client.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, client.Timeout)
		// The body is consumed by the caller, so cancellation must wait until
		// Close rather than invalidating a successful streaming response here.
		response, err := doWebAttempts(client, req.WithContext(ctx))
		if err != nil {
			cancel()
			return nil, err
		}
		response.Body = &cancelOnCloseBody{ReadCloser: response.Body, cancel: cancel}
		return response, nil
	}
	return doWebAttempts(client, req)
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	defer b.cancel()
	return b.ReadCloser.Close()
}

func doWebAttempts(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
	default:
		return resp, nil
	}
	delay := 250 * time.Millisecond
	if value := resp.Header.Get("Retry-After"); value != "" {
		seconds, err := strconv.Atoi(value)
		if err == nil && seconds >= 0 && seconds <= 2 {
			delay = time.Duration(seconds) * time.Second
		} else {
			return resp, nil // do not retry earlier than the server permits
		}
	}
	if req.Body != nil && req.GetBody == nil {
		return resp, nil
	}
	_ = resp.Body.Close()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-timer.C:
	}
	next := req.Clone(req.Context())
	if req.GetBody != nil {
		next.Body, err = req.GetBody()
		if err != nil {
			return nil, err
		}
	}
	return client.Do(next)
}

// finalizeContent applies the shared post-processing: drop ANSI escapes, report
// an empty page, optionally filter by the caller's hint, then trim the budget.
func finalizeContent(content, prompt string) string {
	return finalizeContentWithLimit(content, prompt, maxFetchRunes)
}

func finalizeContentWithLimit(content, prompt string, limit int) string {
	content = strings.TrimSpace(toolutil.StripAnsi(content))

	if content == "" {
		return "Page content is empty"
	}

	if prompt != "" {
		matched := false
		for _, keyword := range strings.Fields(prompt) {
			if strings.Contains(strings.ToLower(content), strings.ToLower(keyword)) {
				matched = true
				break
			}
		}
		content = extractRelevant(content, prompt)
		if !matched {
			content = "[Extraction hint did not match the retrieved content; the requested section may be missing. Returning available content.]\n\n" + content
		}
	}

	return truncateWebContent(content, limit)
}

// Keep the truncation notice inside the caller's content budget. Never let
// a clipped source look like a complete page or search response.
func truncateWebContent(content string, limit int) string {
	if len([]rune(content)) <= limit {
		return content
	}
	const notice = "\n\n[Content truncated; narrow the query or extraction hint for omitted details.]"
	if limit <= len([]rune(notice)) {
		return text.TruncateByRune(notice, limit)
	}
	return text.TruncateByRune(content, limit-len([]rune(notice))) + notice
}

func validateURL(rawURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return validateURLContext(ctx, rawURL)
}

func validateURLContext(ctx context.Context, rawURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(rawURL) > maxWebURLBytes {
		return fmt.Errorf("URL exceeds %d byte limit", maxWebURLBytes)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http/https allowed")
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing hostname")
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("private network access denied")
		}
	} else {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return fmt.Errorf("DNS lookup failed: %w", err)
		}
		for _, ip := range ips {
			if isPrivateIP(ip) {
				return fmt.Errorf("private network access denied")
			}
		}
	}

	return nil
}

func isPrivateIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	privateBlocks := []string{
		"100.64.0.0/10",
		"198.18.0.0/15",
		"fec0::/10",
	}
	for _, cidr := range privateBlocks {
		_, block, _ := net.ParseCIDR(cidr)
		if block != nil && block.Contains(ip) {
			return true
		}
	}
	return false
}

func extractRelevant(content, prompt string) string {
	keywords := strings.Fields(prompt)
	if len(keywords) == 0 {
		return content
	}

	paragraphs := markdownParagraphs(content)
	selected := make([]bool, len(paragraphs))
	var headings [7]int
	for i := range headings {
		headings[i] = -1
	}
	found := false
	for i, p := range paragraphs {
		if level := paragraphHeading(p); level > 0 {
			headings[level] = i
			for deeper := level + 1; deeper < len(headings); deeper++ {
				headings[deeper] = -1
			}
		}
		pLower := strings.ToLower(p)
		for _, kw := range keywords {
			if strings.Contains(pLower, strings.ToLower(kw)) {
				found = true
				selected[i] = true
				for _, index := range headings {
					if index >= 0 {
						selected[index] = true
					}
				}
				// Keep adjacent explanation/warnings, without crossing a section.
				if i > 0 && paragraphHeading(p) == 0 && paragraphHeading(paragraphs[i-1]) == 0 {
					selected[i-1] = true
				}
				if i+1 < len(paragraphs) && paragraphHeading(paragraphs[i+1]) == 0 {
					selected[i+1] = true
				}
				break
			}
		}
	}
	if !found {
		return content
	}
	var relevant []string
	for i, paragraph := range paragraphs {
		if selected[i] {
			relevant = append(relevant, paragraph)
		}
	}
	return strings.Join(relevant, "\n\n")
}

// Blank lines delimit prose paragraphs, but belong to a fenced code block.
// Keeping the block atomic also prevents code comments becoming headings.
func markdownParagraphs(content string) []string {
	var paragraphs []string
	var lines []string
	var fence byte
	fenceLength := 0
	flush := func() {
		if len(lines) > 0 {
			paragraphs = append(paragraphs, strings.Join(lines, "\n"))
			lines = nil
		}
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != 0 {
			lines = append(lines, line)
			run := 0
			for run < len(trimmed) && trimmed[run] == fence {
				run++
			}
			if run >= fenceLength && strings.TrimSpace(trimmed[run:]) == "" {
				fence = 0
				flush()
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			flush()
			fence = trimmed[0]
			fenceLength = 0
			for fenceLength < len(trimmed) && trimmed[fenceLength] == fence {
				fenceLength++
			}
			lines = append(lines, line)
		} else if trimmed == "" {
			flush()
		} else {
			lines = append(lines, line)
		}
	}
	flush()
	return paragraphs
}

func paragraphHeading(paragraph string) int {
	line := strings.SplitN(strings.TrimSpace(paragraph), "\n", 2)[0]
	level := len(line) - len(strings.TrimLeft(line, "#"))
	if level > 0 && level <= 6 && len(line) > level && line[level] == ' ' {
		return level
	}
	return 0
}
