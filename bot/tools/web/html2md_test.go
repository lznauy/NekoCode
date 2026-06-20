package web

import (
	"strings"
	"testing"
)

func TestHtml2md(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
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
			got := strings.TrimSpace(html2md(tt.in))
			if got != tt.want {
				t.Errorf("\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}
