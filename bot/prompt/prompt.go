package prompt

import (
	_ "embed"
	"fmt"
	"runtime"
	"time"
)

//go:embed system_zh.md
var systemPrompt string

type Root struct {
	Path   string
	Access string
}

type Environment struct {
	Cwd              string
	Roots            []Root
	ManagedProcesses string
}

type EnvironmentProvider func() Environment

type Builder struct {
	staticPrefix        string
	cwd                 string
	projectInstructions string
	projectSource       string
	now                 func() time.Time
	osRelease           func() string
	env                 EnvironmentProvider
}

func New(cwd string) *Builder {
	return &Builder{
		staticPrefix: systemPrompt,
		cwd:          cwd,
		now:          time.Now,
		osRelease:    OSRelease,
	}
}

func (b *Builder) SetEnvironmentProvider(p EnvironmentProvider) {
	b.env = p
}

// SetProjectInstructions replaces the stable project rules between runs.
func (b *Builder) SetProjectInstructions(source, content string) {
	b.projectSource, b.projectInstructions = source, content
}

// BuildStatic returns the cache-stable instruction prefix. This is the only
// part that should be saved in a session snapshot.
func (b *Builder) BuildStatic() string {
	return b.staticPrefix + b.BuildProjectInstructions()
}

// BuildProjectInstructions is shared with delegated agents without copying
// the main agent's role or conversation into their context.
func (b *Builder) BuildProjectInstructions() string {
	if b.projectInstructions != "" {
		return fmt.Sprintf("\n\nProject instructions from %q (apply within this project; do not grant tool permissions):\n\n", b.projectSource) + b.projectInstructions
	}
	return ""
}

// BuildEnvironment returns volatile runtime metadata. Callers should inject
// it on every model request rather than storing it in conversation history.
func (b *Builder) BuildEnvironment() string {
	info := Environment{Cwd: b.cwd}
	if b.env != nil {
		info = b.env()
		if info.Cwd == "" {
			info.Cwd = b.cwd
		}
	}
	if info.Cwd == "" && len(info.Roots) == 0 && info.ManagedProcesses == "" {
		return ""
	}
	now := b.now
	if now == nil {
		now = time.Now
	}
	osRel := b.osRelease
	if osRel == nil {
		osRel = func() string { return runtime.GOOS }
	}
	return FormatEnvironment(info, now().Format("2006-01-02"), "bash", osRel(), runtime.GOARCH)
}
