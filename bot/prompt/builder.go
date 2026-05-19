package prompt

import (
	_ "embed"
	"strings"
	"time"

	ctxfmt "nekocode/bot/ctxmgr/context"
)

//go:embed system_zh.md
var systemPrompt string

type Builder struct {
	staticPrefix string
	cwd          string // set once at init, used on every Build() with fresh date
}

func NewBuilder(cwd string) *Builder {
	return &Builder{staticPrefix: systemPrompt, cwd: cwd}
}

func (b *Builder) Build() string {
	var parts []string
	if b.staticPrefix != "" {
		parts = append(parts, b.staticPrefix)
	}
	if b.cwd != "" {
		now := time.Now().Format("2006-01-02")
		parts = append(parts, ctxfmt.FormatEnv(b.cwd, now))
	}
	return strings.Join(parts, "\n\n")
}
