// header.go — 顶部标题栏（provider / model / version / tokens）。
package components
import (
	"fmt"
	"strings"

	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"
)

type Header struct {
	Width    int
	Provider string
	Model    string
	Version  string
	Tokens   int
}

func NewHeader(width int, provider, model, version string) *Header {
	return &Header{
		Width:    width,
		Provider: provider,
		Model:    model,
		Version:  version,
	}
}

func (h *Header) SetWidth(width int)  { h.Width = width }
func (h *Header) SetTokens(total int) { h.Tokens = total }
func (h *Header) SetModel(provider, model string) { h.Provider = provider; h.Model = model }
func (h *Header) Height() int         { return 2 }

func (h *Header) View() string {
	w := max(20, h.Width)

	catIcon := styles.CatBodyStyle.Render("(=") + styles.CatEyeStyle.Render("^.^") + styles.CatBodyStyle.Render("=)")
	left := catIcon + " " + styles.PrimaryStyle.Bold(true).Render("NEKOCODE") + " " + styles.SubtleStyle.Render("v"+h.Version)
	right := styles.MutedStyle.Render(h.Provider + "/" + h.Model)
	dot := styles.BorderStyle.Render(" · ")

	if h.Tokens > 0 {
		right = styles.FmtTokens(h.Tokens) + dot + right
	}

	content := left + dot + right
	contentW := lipgloss.Width(content)
	pad := max(0, w-contentW)

	line := strings.Repeat(styles.Horizontal, w)

	var b strings.Builder
	fmt.Fprintf(&b, "%s%s\n", content, strings.Repeat(" ", pad))
	fmt.Fprintf(&b, "%s\n", styles.BorderStyle.Render(line))

	return b.String()
}
