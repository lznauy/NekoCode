package system

import (
	_ "embed"
	"runtime"
	"strings"
	"time"

	ctxfmt "nekocode/bot/contextmgr/context"
)

//go:embed system_zh.md
var systemPrompt string

type Builder struct {
	staticPrefix string
	cwd          string
	now          func() time.Time
	osRelease    func() string
}

func NewBuilder(cwd string) *Builder {
	return &Builder{
		staticPrefix: systemPrompt,
		cwd:          cwd,
		now:          time.Now,
		osRelease:    OSRelease,
	}
}

func NewTestBuilder(cwd, staticPrefix string, now func() time.Time, osRelease func() string) *Builder {
	return &Builder{
		staticPrefix: staticPrefix,
		cwd:          cwd,
		now:          now,
		osRelease:    osRelease,
	}
}

func (b *Builder) Build() string {
	var parts []string
	if b.staticPrefix != "" {
		parts = append(parts, b.staticPrefix)
	}
	if b.cwd != "" {
		now := b.now
		if now == nil {
			now = time.Now
		}
		osRel := b.osRelease
		if osRel == nil {
			osRel = func() string { return runtime.GOOS }
		}
		date := now().Format("2006-01-02")
		parts = append(parts, ctxfmt.FormatEnv(b.cwd, date, osRel(), runtime.GOARCH))
	}
	return strings.Join(parts, "\n\n")
}
