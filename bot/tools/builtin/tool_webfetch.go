package builtin

import (
	"context"
	"fmt"
	"io"
	"nekocode/bot/tools"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nekocode/common"
)

type WebFetchTool struct {
	client *http.Client
}

func NewWebFetchTool() *WebFetchTool {
	c := tools.NewToolHTTPClient(15 * time.Second)
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}
	return &WebFetchTool{client: c}
}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) ExecutionMode(map[string]any) tools.ExecutionMode {
	return tools.ModeParallel
}
func (t *WebFetchTool) DangerLevel(map[string]any) common.DangerLevel {
	return common.LevelSafe
}

func (t *WebFetchTool) Description() string {
	return "Fetch web page as text. When quoting, cite source URL and keep quotes ≤125 chars."
}

func (t *WebFetchTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "url", Type: "string", Required: true, Description: "Web page URL to fetch"},
		{Name: "prompt", Type: "string", Required: false, Description: "Content extraction hint, e.g. 'extract API parameters'"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	rawURL, ok := args["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return "", fmt.Errorf("missing url parameter")
	}

	if err := validateURL(rawURL); err != nil {
		return "", fmt.Errorf("URL validation failed: %v", err)
	}

	prompt, _ := args["prompt"].(string)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build request: %v", err)
	}
	req.Header.Set("User-Agent", "NekoCode/1.0")
	req.Header.Set("Accept", "text/html,text/plain,*/*")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	contentType := resp.Header.Get("Content-Type")
	var content string
	if strings.Contains(contentType, "text/html") {
		content = html2md(string(body))
	} else {
		content = string(body)
	}

	content = tools.StripAnsi(content)

	if content == "" {
		return "Page content is empty", nil
	}

	if prompt != "" {
		content = extractRelevant(content, prompt)
	}

	content = common.TruncateByRune(content, 3000)
	return content, nil
}

func validateURL(rawURL string) error {
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
		ips, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("DNS lookup failed: %v", err)
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
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"fc00::/7",
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

	paragraphs := strings.Split(content, "\n\n")
	var relevant []string
	for _, p := range paragraphs {
		pLower := strings.ToLower(p)
		for _, kw := range keywords {
			if strings.Contains(pLower, strings.ToLower(kw)) {
				relevant = append(relevant, p)
				break
			}
		}
	}
	if len(relevant) == 0 {
		return content
	}
	return strings.Join(relevant, "\n\n")
}
