package web

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHTMLNestedListsDoNotExpandQuadratically(t *testing.T) {
	input := strings.Repeat("<ul><li>x", 1000) + strings.Repeat("</li></ul>", 1000)
	got, err := html2md(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 100000 {
		t.Fatalf("%d byte page expanded to %d bytes", len(input), len(got))
	}
}

func TestHTMLConversionOutputLimitAndCancellation(t *testing.T) {
	if got, err := html2md(context.Background(), "<pre>"+strings.Repeat("`", maxHTMLMarkdownBytes/3)+"</pre>"); err == nil || got != "" {
		t.Fatalf("fence expansion bypassed output limit: %d bytes %v", len(got), err)
	}
	if got, err := html2md(context.Background(), "<p>"+strings.Repeat("x", maxHTMLMarkdownBytes+1)+"</p>"); err == nil || got != "" {
		t.Fatalf("plain text bypassed output limit: %d bytes %v", len(got), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := html2md(ctx, "<p>text</p>"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled conversion continued: %v", err)
	}
}

func TestHtml2md(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "inline code preserves spaces and strips highlighting",
			in:   "<p><code><span> value </span></code> and <code>line1\nline2</code></p>",
			want: "`  value  ` and `line1 line2`",
		},
		{
			name: "empty inline code adds no delimiters",
			in:   `<p>Before<code></code>After</p>`,
			want: "BeforeAfter",
		},
		{
			name: "inline code containing backticks",
			in:   "<p>Use <code>`token`</code> literally.</p>",
			want: "Use `` `token` `` literally.",
		},
		{
			name: "ordered list numbering",
			in:   `<ol start="3"><li>Third</li><li value="7">Seventh</li><li>Eighth</li></ol>`,
			want: "3. Third\n7. Seventh\n8. Eighth",
		},
		{
			name: "nested list numbering",
			in:   `<ol><li>First<ul><li>Child</li></ul></li><li>Second</li></ol>`,
			want: "1. First\n  - Child\n2. Second",
		},
		{
			name: "article header retains title and date",
			in:   `<header>site menu</header><article><header><h1>Report</h1><p>2026-11-06</p></header><p>Details</p></article>`,
			want: "# Report\n\n2026-11-06\n\nDetails",
		},
		{
			name: "head metadata excluded",
			in:   `<html><head><title>metadata</title></head><body><p>Body</p></body></html>`,
			want: "Body",
		},
		{
			name: "highlighted code preserves whitespace",
			in:   "<pre><code><span>one</span>\n\n\n  two &lt; three</code></pre>",
			want: "```\none\n\n\n  two < three\n```",
		},
		{
			name: "code containing markdown fence",
			in:   "<pre><code>```go\nexample\n```</code></pre>",
			want: "````\n```go\nexample\n```\n````",
		},
		{
			name: "table cells retain boundaries",
			in:   `<table><tr><th>Name</th><th>Value</th></tr><tr><td>timeout</td><td>30s</td></tr></table>`,
			want: "| Name | Value |\n| timeout | 30s |",
		},
		{
			name: "headings",
			in:   "<h1>Title</h1><h2>Section</h2>",
			want: "# Title\n\n## Section",
		},
		{
			name: "paragraphs",
			in:   "<p>Hello</p><p>World</p>",
			want: "Hello\n\nWorld",
		},
		{
			name: "link",
			in:   `<a href="https://example.com">click here</a>`,
			want: "[click here](https://example.com)",
		},
		{
			name: "code and pre",
			in:   "use <code>fmt.Println()</code> for output",
			want: "use `fmt.Println()` for output",
		},
		{
			name: "bold and italic",
			in:   "<strong>bold</strong> and <em>italic</em>",
			want: "**bold** and *italic*",
		},
		{
			name: "list",
			in:   "<ul><li>one</li><li>two</li></ul>",
			want: "- one\n- two",
		},
		{
			name: "skip script and style",
			in:   "<p>visible</p><script>hidden</script><style>also hidden</style><p>more</p>",
			want: "visible\n\nmore",
		},
		{
			name: "image",
			in:   `<img src="pic.png" alt="a photo">`,
			want: "![a photo](pic.png)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := html2md(context.Background(), tt.in)
			if err != nil {
				t.Fatal(err)
			}
			got = strings.TrimSpace(got)
			if got != tt.want {
				t.Errorf("\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}
