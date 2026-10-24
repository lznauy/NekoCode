package message

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

// nekocodeStyle is a custom glamour StyleConfig tuned for the "深夜书房" TUI
// theme. It keeps the structural formatting (table layout, list bullets,
// heading prefixes) from glamour's dark styles but re-maps the color palette
// to the project's teal / dark-gold / cat-eye-blue accents so rendered
// markdown blends with the surrounding chat UI instead of clashing with it.
var nekocodeStyle = ansi.StyleConfig{
	Document: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "\n",
			BlockSuffix: "\n",
			Color:       stringPtr("#a0a0a0"),
		},
		Margin: uintPtr(defaultMargin),
	},
	BlockQuote: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color: stringPtr("#808080"),
		},
		Indent:      uintPtr(1),
		IndentToken: stringPtr("│ "),
	},
	List: ansi.StyleList{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("#a0a0a0"),
			},
		},
		LevelIndent: defaultListIndent,
	},
	Heading: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n",
			Color:       stringPtr("#4ec9b0"),
			Bold:        boolPtr(true),
		},
	},
	H1: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "# ",
			Bold:   boolPtr(true),
		},
	},
	H2: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "## ",
		},
	},
	H3: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "### ",
		},
	},
	H4: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "#### ",
		},
	},
	H5: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "##### ",
		},
	},
	H6: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "###### ",
		},
	},
	Strikethrough: ansi.StylePrimitive{
		CrossedOut: boolPtr(true),
	},
	Emph: ansi.StylePrimitive{
		Italic: boolPtr(true),
	},
	Strong: ansi.StylePrimitive{
		Bold: boolPtr(true),
	},
	HorizontalRule: ansi.StylePrimitive{
		Color:  stringPtr("#333333"),
		Format: "\n────────────────────────────────\n",
	},
	Item: ansi.StylePrimitive{
		BlockPrefix: "• ",
	},
	Enumeration: ansi.StylePrimitive{
		BlockPrefix: ". ",
		Color:       stringPtr("#c9a96e"),
	},
	Task: ansi.StyleTask{
		StylePrimitive: ansi.StylePrimitive{},
		Ticked:         "✓ ",
		Unticked:       "· ",
	},
	Link: ansi.StylePrimitive{
		Color:     stringPtr("#7ec8e3"),
		Underline: boolPtr(true),
	},
	LinkText: ansi.StylePrimitive{
		Color: stringPtr("#7ec8e3"),
	},
	Image: ansi.StylePrimitive{
		Color:     stringPtr("#7ec8e3"),
		Underline: boolPtr(true),
	},
	ImageText: ansi.StylePrimitive{
		Color:  stringPtr("#7ec8e3"),
		Format: "image: {{.text}}",
	},
	Code: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color: stringPtr("#c9a96e"),
		},
	},
	CodeBlock: ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("#d4b85a"),
			},
			Margin: uintPtr(defaultMargin),
		},
		Chroma: &ansi.Chroma{
			Text: ansi.StylePrimitive{
				Color: stringPtr("#a0a0a0"),
			},
			Error: ansi.StylePrimitive{
				Color: stringPtr("#e06c75"),
			},
			Comment: ansi.StylePrimitive{
				Color: stringPtr("#666666"),
			},
			CommentPreproc: ansi.StylePrimitive{
				Color: stringPtr("#7ec8e3"),
			},
			Keyword: ansi.StylePrimitive{
				Color: stringPtr("#7ec8e3"),
			},
			KeywordReserved: ansi.StylePrimitive{
				Color: stringPtr("#7ec8e3"),
			},
			KeywordNamespace: ansi.StylePrimitive{
				Color: stringPtr("#7ec8e3"),
			},
			KeywordType: ansi.StylePrimitive{
				Color: stringPtr("#c9a96e"),
			},
			Operator: ansi.StylePrimitive{
				Color: stringPtr("#4ec9b0"),
			},
			Punctuation: ansi.StylePrimitive{
				Color: stringPtr("#808080"),
			},
			Name: ansi.StylePrimitive{
				Color: stringPtr("#a0a0a0"),
			},
			NameConstant: ansi.StylePrimitive{
				Color: stringPtr("#4ec9b0"),
			},
			NameBuiltin: ansi.StylePrimitive{
				Color: stringPtr("#c9a96e"),
			},
			NameTag: ansi.StylePrimitive{
				Color: stringPtr("#7ec8e3"),
			},
			NameAttribute: ansi.StylePrimitive{
				Color: stringPtr("#98c379"),
			},
			NameClass: ansi.StylePrimitive{
				Color: stringPtr("#4ec9b0"),
			},
			NameDecorator: ansi.StylePrimitive{
				Color: stringPtr("#98c379"),
			},
			NameFunction: ansi.StylePrimitive{
				Color: stringPtr("#4ec9b0"),
			},
			LiteralNumber: ansi.StylePrimitive{
				Color: stringPtr("#d4b85a"),
			},
			LiteralString: ansi.StylePrimitive{
				Color: stringPtr("#98c379"),
			},
			LiteralStringEscape: ansi.StylePrimitive{
				Color: stringPtr("#7ec8e3"),
			},
			GenericDeleted: ansi.StylePrimitive{
				Color: stringPtr("#e06c75"),
			},
			GenericEmph: ansi.StylePrimitive{
				Italic: boolPtr(true),
			},
			GenericInserted: ansi.StylePrimitive{
				Color: stringPtr("#98c379"),
			},
			GenericStrong: ansi.StylePrimitive{
				Bold: boolPtr(true),
			},
			GenericSubheading: ansi.StylePrimitive{
				Color: stringPtr("#4ec9b0"),
			},
			Background: ansi.StylePrimitive{
				BackgroundColor: stringPtr("#1a1b26"),
			},
		},
	},
	Table: ansi.StyleTable{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
		},
	},
	DefinitionDescription: ansi.StylePrimitive{
		BlockPrefix: "→ ",
	},
}

// Reuse glamour's default constants so structural formatting (margins,
// indents) stays consistent with the upstream dark styles.
const (
	defaultMargin     = 2
	defaultListIndent = 2
)

func boolPtr(b bool) *bool       { return &b }
func stringPtr(s string) *string { return &s }
func uintPtr(u uint) *uint       { return &u }

var (
	mu        sync.Mutex
	renderMu  sync.Mutex
	renderers = map[int]*glamour.TermRenderer{}
)

func Warmup() {
	mu.Lock()
	defer mu.Unlock()
	renderers = map[int]*glamour.TermRenderer{}
	for w := 40; w <= 160; w++ {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(nekocodeStyle),
			glamour.WithWordWrap(w),
		)
		if err != nil {
			panic("failed to warm up markdown renderer: " + err.Error())
		}
		renderers[w] = r
	}
}

func getRenderer(width int) *glamour.TermRenderer {
	mu.Lock()
	defer mu.Unlock()
	if r, ok := renderers[width]; ok {
		return r
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(nekocodeStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		panic("failed to create markdown renderer: " + err.Error())
	}
	renderers[width] = r
	return r
}

func RenderMarkdown(content string, width int) string {
	if width <= 0 {
		width = 80
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	// Cached glamour renderers hold mutable parser state. History preparation
	// can now overlap the foreground UI, so serialize access to them.
	renderMu.Lock()
	defer renderMu.Unlock()
	out, err := getRenderer(width).Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSpace(out)
}
