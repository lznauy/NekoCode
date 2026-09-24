package message

import (
	"strings"
	"testing"

	"nekocode/interaction/tui/styles"
)

func TestSystemContentHyperlinksBareURL(t *testing.T) {
	sty := styles.DefaultStyles()
	const link = "https://openapi.tuniu.cn/oauth2/auth?client_id=app_dyn&code_challenge=abc&redirect_uri=http%3A%2F%2F127.0.0.1%3A35495%2Foauth%2Fcallback"
	content := "tuniu 授权链接已生成（完成后自动就绪）：\n" + link
	out := renderSystemContent(content, 80, &sty)
	if !strings.Contains(out, "打开授权链接") {
		t.Fatalf("hyperlink label missing: %q", out)
	}
	if !strings.Contains(out, "\x1b]8;;"+link+"\x07") {
		t.Fatalf("OSC 8 hyperlink missing: %q", out)
	}
	if strings.Count(out, "openapi.tuniu.cn") != 1 {
		t.Fatalf("URL should appear only inside the hyperlink sequence: %q", out)
	}
}

func TestSystemContentKeepsProseMarkdown(t *testing.T) {
	sty := styles.DefaultStyles()
	out := renderSystemContent("第一行说明\n第二行说明", 80, &sty)
	if !strings.Contains(out, "第一行说明") || !strings.Contains(out, "第二行说明") {
		t.Fatalf("prose lost: %q", out)
	}
	if strings.Contains(out, "\x1b]8;;") {
		t.Fatalf("no hyperlink expected for prose: %q", out)
	}
}

func TestIsBareURLRejectsControlChars(t *testing.T) {
	if !isBareURL("https://example.com/oauth2/auth?x=1") {
		t.Fatal("plain https URL should match")
	}
	if isBareURL("https://evil.example/\x1b]8;;\x07more") {
		t.Fatal("control characters must be rejected")
	}
	if isBareURL("https://example.com with space") {
		t.Fatal("URL with spaces is not a bare URL line")
	}
	if isBareURL("ftp://example.com/file") {
		t.Fatal("non-http scheme should not match")
	}
}

func TestHyperlinkURLLinesInRenderedContent(t *testing.T) {
	sty := styles.DefaultStyles()
	const link = "https://openapi.tuniu.cn/oauth2/auth?client_id=app_dyn&code_challenge=abc"
	content := "tuniu 授权链接已生成（完成后自动就绪）：\n" + link
	out := hyperlinkURLLines(content, &sty)
	if !strings.Contains(out, "\x1b]8;;"+link+"\x07") || !strings.Contains(out, "打开授权链接") {
		t.Fatalf("rendered content path did not hyperlink URL: %q", out)
	}
	if !strings.Contains(out, "授权链接已生成") {
		t.Fatalf("prose lost: %q", out)
	}
}
