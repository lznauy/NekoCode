package web

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const maxHTMLMarkdownBytes = 8 << 20

func html2md(ctx context.Context, rawHTML string) (string, error) {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(rawHTML))
	var linkHref string
	var linkOpen bool
	var pre strings.Builder
	var inline strings.Builder
	inCode := false
	type listState struct {
		ordered bool
		next    int
	}
	var lists []listState
	inPre := false
	contentDepth := 0
	skipStack := 0
	var last byte // track last byte to avoid b.String() calls in ensureNewline

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if b.Len()+pre.Len()+inline.Len() > maxHTMLMarkdownBytes {
			return "", fmt.Errorf("HTML conversion exceeds %d byte limit", maxHTMLMarkdownBytes)
		}
		tt := z.Next()
		if tt == html.ErrorToken {
			if inPre {
				if err := writePre(&b, &last, pre.String()); err != nil {
					return "", err
				}
			}
			if inCode {
				if err := writeInlineCode(&b, &last, inline.String()); err != nil {
					return "", err
				}
			}
			break
		}
		if inPre {
			switch tt {
			case html.TextToken:
				pre.Write(z.Text())
			case html.StartTagToken, html.SelfClosingTagToken:
				name, _ := z.TagName()
				if atom.Lookup(name) == atom.Br {
					pre.WriteByte('\n')
				}
			case html.EndTagToken:
				name, _ := z.TagName()
				if atom.Lookup(name) == atom.Pre {
					if err := writePre(&b, &last, pre.String()); err != nil {
						return "", err
					}
					inPre = false
					pre.Reset()
				}
			}
			continue
		}
		if inCode {
			if tt == html.TextToken {
				inline.Write(z.Text())
			}
			if tt == html.EndTagToken {
				name, _ := z.TagName()
				if atom.Lookup(name) == atom.Code {
					if err := writeInlineCode(&b, &last, inline.String()); err != nil {
						return "", err
					}
					inCode = false
					inline.Reset()
				}
			}
			continue
		}

		if skipStack > 0 {
			switch tt {
			case html.EndTagToken:
				name, _ := z.TagName()
				if isSkipAtom(atom.Lookup(name)) {
					skipStack--
				}
			case html.StartTagToken:
				name, _ := z.TagName()
				if isSkipAtom(atom.Lookup(name)) {
					skipStack++
				}
			}
			continue
		}

		switch tt {
		case html.TextToken:
			t := string(z.Text())
			if len(t) > 0 {
				last = t[len(t)-1]
			}
			b.WriteString(t)

		case html.StartTagToken:
			name, hasAttr := z.TagName()
			a := atom.Lookup(name)
			if a == atom.Article || a == atom.Main {
				contentDepth++
			}

			var attrs map[string]string
			if hasAttr {
				attrs = getAttrs(z)
			}

			switch a {
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				ensureNewline(&b, &last)
				fmt.Fprintf(&b, "%s ", strings.Repeat("#", headingLevel(a)))
			case atom.P, atom.Div, atom.Section, atom.Article:
				ensureNewline(&b, &last)
			case atom.Br:
				b.WriteByte('\n')
				last = '\n'
			case atom.Li:
				ensureNewline(&b, &last)
				if len(lists) > 1 {
					b.WriteString(strings.Repeat("  ", min(len(lists)-1, 32)))
				}
				if len(lists) > 0 && lists[len(lists)-1].ordered {
					list := &lists[len(lists)-1]
					if value, err := strconv.Atoi(attrs["value"]); err == nil {
						list.next = value
					}
					fmt.Fprintf(&b, "%d. ", list.next)
					list.next++
				} else {
					b.WriteString("- ")
				}
				last = ' '
			case atom.Code:
				inline.Reset()
				inCode = true
			case atom.Pre:
				pre.Reset()
				inPre = true
			case atom.Strong, atom.B:
				b.WriteString("**")
				last = '*'
			case atom.Em, atom.I:
				b.WriteByte('*')
				last = '*'
			case atom.A:
				linkHref = ""
				if attrs != nil {
					linkHref = attrs["href"]
				}
				linkOpen = linkHref != ""
				if linkOpen {
					b.WriteByte('[')
					last = '['
				}
			case atom.Img:
				alt := "image"
				src := ""
				if attrs != nil {
					if a, ok := attrs["alt"]; ok && a != "" {
						alt = a
					}
					src = attrs["src"]
				}
				fmt.Fprintf(&b, "\n![%s](%s)\n", alt, src)
				last = '\n'
			case atom.Hr:
				b.WriteString("\n---\n")
				last = '\n'
			case atom.Blockquote:
				b.WriteString("\n> ")
				last = ' '
			case atom.Ul, atom.Ol:
				list := listState{ordered: a == atom.Ol, next: 1}
				if start, err := strconv.Atoi(attrs["start"]); err == nil {
					list.next = start
				}
				lists = append(lists, list)
			case atom.Tr:
				ensureNewline(&b, &last)
				b.WriteByte('|')
				last = '|'
			case atom.Td, atom.Th:
				b.WriteByte(' ')
				last = ' '
			case atom.Header:
				if contentDepth == 0 {
					skipStack++
				}
			case atom.Head, atom.Script, atom.Style, atom.Svg, atom.Nav, atom.Footer, atom.Aside, atom.Noscript:
				skipStack++
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			a := atom.Lookup(name)
			if (a == atom.Article || a == atom.Main) && contentDepth > 0 {
				contentDepth--
			}
			switch a {
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				b.WriteString("\n\n")
				last = '\n'
			case atom.P, atom.Div, atom.Section, atom.Article:
				b.WriteString("\n\n")
				last = '\n'
			case atom.Ul, atom.Ol:
				if len(lists) > 0 {
					lists = lists[:len(lists)-1]
				}
				ensureNewline(&b, &last)
			case atom.Strong, atom.B:
				b.WriteString("**")
				last = '*'
			case atom.Em, atom.I:
				b.WriteByte('*')
				last = '*'
			case atom.A:
				if linkOpen {
					fmt.Fprintf(&b, "](%s)", linkHref)
					last = ')'
				}
				linkHref = ""
				linkOpen = false
			case atom.Td, atom.Th:
				b.WriteString(" |")
				last = '|'
			case atom.Tr:
				b.WriteByte('\n')
				last = '\n'
			}

		case html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			a := atom.Lookup(name)

			var attrs map[string]string
			if hasAttr {
				attrs = getAttrs(z)
			}

			switch a {
			case atom.Br:
				b.WriteByte('\n')
				last = '\n'
			case atom.Hr:
				b.WriteString("\n---\n")
				last = '\n'
			case atom.Img:
				alt := "image"
				src := ""
				if attrs != nil {
					if a, ok := attrs["alt"]; ok && a != "" {
						alt = a
					}
					src = attrs["src"]
				}
				fmt.Fprintf(&b, "\n![%s](%s)\n", alt, src)
				last = '\n'
			}
		}
	}

	if b.Len() > maxHTMLMarkdownBytes {
		return "", fmt.Errorf("HTML conversion exceeds %d byte limit", maxHTMLMarkdownBytes)
	}
	return collapseBlankLines(strings.TrimSpace(b.String())), nil
}

func ensureNewline(b *strings.Builder, last *byte) {
	if *last != 0 && *last != '\n' {
		b.WriteByte('\n')
		*last = '\n'
	}
}

func getAttrs(z *html.Tokenizer) map[string]string {
	m := make(map[string]string)
	for {
		k, v, more := z.TagAttr()
		m[string(k)] = string(v)
		if !more {
			break
		}
	}
	return m
}

func headingLevel(a atom.Atom) int {
	switch a {
	case atom.H1:
		return 1
	case atom.H2:
		return 2
	case atom.H3:
		return 3
	case atom.H4:
		return 4
	case atom.H5:
		return 5
	case atom.H6:
		return 6
	}
	return 1
}

func isSkipAtom(a atom.Atom) bool {
	switch a {
	case atom.Head, atom.Script, atom.Style, atom.Svg, atom.Nav, atom.Footer, atom.Header, atom.Aside, atom.Noscript:
		return true
	}
	return false
}

func collapseBlankLines(s string) string {
	var lines []string
	fence := ""
	blank := false
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			lines = append(lines, line)
			if trimmed == fence {
				fence = ""
			}
			blank = false
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = trimmed
		}
		if trimmed != "" || !blank {
			lines = append(lines, line)
		}
		blank = trimmed == ""
	}
	return strings.Join(lines, "\n")
}

func longestBacktickRun(content string) int {
	longest, run := 0, 0
	for _, r := range content {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return longest
}

func writeInlineCode(b *strings.Builder, last *byte, content string) error {
	content = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(content)
	if content == "" {
		return nil
	}
	fenceLength := longestBacktickRun(content) + 1
	if b.Len()+len(content)+2*fenceLength+2 > maxHTMLMarkdownBytes {
		return fmt.Errorf("HTML conversion exceeds %d byte limit", maxHTMLMarkdownBytes)
	}
	fence := strings.Repeat("`", fenceLength)
	pad := strings.HasPrefix(content, "`") || strings.HasSuffix(content, "`") ||
		(strings.HasPrefix(content, " ") && strings.HasSuffix(content, " ") && strings.TrimSpace(content) != "")
	b.WriteString(fence)
	if pad {
		b.WriteByte(' ')
	}
	b.WriteString(content)
	if pad {
		b.WriteByte(' ')
	}
	b.WriteString(fence)
	*last = '`'
	return nil
}

func writePre(b *strings.Builder, last *byte, content string) error {
	longest := longestBacktickRun(content)
	length := 3
	if longest >= length {
		length = longest + 1
	}
	if b.Len()+len(content)+2*length+6 > maxHTMLMarkdownBytes {
		return fmt.Errorf("HTML conversion exceeds %d byte limit", maxHTMLMarkdownBytes)
	}
	fence := strings.Repeat("`", length)
	b.WriteString("\n\n" + fence + "\n")
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(fence + "\n\n")
	*last = '\n'
	return nil
}
