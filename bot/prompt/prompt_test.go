package prompt

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"
)

// This golden locks the immutable provider prefix byte-for-byte. Adding a
// date, environment probe, map-rendered block, or any other dynamic content
// to system_zh.md/BuildStatic must fail here and be reviewed explicitly.
func TestStaticPromptBytesAreStable(t *testing.T) {
	runtimeCalls := 0
	first := &Builder{
		staticPrefix: systemPrompt,
		cwd:          "/first",
		now: func() time.Time {
			runtimeCalls++
			return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		},
		osRelease: func() string { runtimeCalls++; return "first-os" },
		env:       func() Environment { runtimeCalls++; return Environment{Cwd: "/first"} },
	}
	second := &Builder{
		staticPrefix: systemPrompt,
		cwd:          "/second",
		now: func() time.Time {
			runtimeCalls++
			return time.Date(2030, 12, 31, 0, 0, 0, 0, time.UTC)
		},
		osRelease: func() string { runtimeCalls++; return "second-os" },
		env:       func() Environment { runtimeCalls++; return Environment{Cwd: "/second"} },
	}
	one, two := first.BuildStatic(), second.BuildStatic()
	if one != two || runtimeCalls != 0 {
		t.Fatalf("BuildStatic depends on runtime state: equal=%v runtimeCalls=%d", one == two, runtimeCalls)
	}
	const wantSHA256 = "d6132a40009842ac6562319d50d0254bc256bf3152a6c76f75a269a098ff3a87"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(one))); got != wantSHA256 {
		t.Fatalf("static prompt bytes changed: sha256=%s, want %s", got, wantSHA256)
	}
}

func TestBuilderInjectsEnvironmentBlock(t *testing.T) {
	b := &Builder{
		cwd:          "/repo",
		staticPrefix: "static",
		now:          func() time.Time { return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC) },
		osRelease:    func() string { return "test-os" },
		env: func() Environment {
			return Environment{
				Cwd:   "/repo",
				Roots: []Root{{Path: "/repo", Access: "read-write"}},
			}
		},
	}

	got := b.BuildEnvironment()
	if !strings.Contains(got, "<environment_context>") {
		t.Fatalf("missing environment block: %q", got)
	}
	if strings.Count(got, "<cwd>/repo</cwd>") != 1 {
		t.Fatalf("cwd should be injected once: %q", got)
	}
}

func TestBuilderWithoutEnvProvider(t *testing.T) {
	b := &Builder{
		cwd:          "/repo",
		staticPrefix: "static",
		now:          func() time.Time { return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC) },
		osRelease:    func() string { return "test-os" },
	}

	got := b.BuildEnvironment()
	if !strings.Contains(got, "<environment_context>") || !strings.Contains(got, "<cwd>/repo</cwd>") ||
		!strings.Contains(got, "2026-06-19") || !strings.Contains(got, "test-os") {
		t.Fatalf("builder should still inject basic runtime facts: %q", got)
	}
	if strings.Contains(got, "<workspace_roots>") {
		t.Fatalf("builder should not invent workspace roots: %q", got)
	}
}

func TestBuilderSeparatesStableAndRuntimeContent(t *testing.T) {
	b := &Builder{
		cwd:          "/repo",
		staticPrefix: "stable rules",
		now:          func() time.Time { return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC) },
		osRelease:    func() string { return "test-os" },
		env: func() Environment {
			return Environment{
				Cwd: "/repo", Roots: []Root{{Path: "/repo", Access: "read-write"}},
			}
		},
	}

	if got := b.BuildStatic(); got != "stable rules" {
		t.Fatalf("BuildStatic() = %q", got)
	}
	runtime := b.BuildEnvironment()
	if strings.Contains(runtime, "stable rules") || !strings.Contains(runtime, "<environment_context>") || !strings.Contains(runtime, "<workspace_roots>") {
		t.Fatalf("BuildEnvironment() = %q", runtime)
	}
}

func TestBuilderInjectsVolatileManagedProcessState(t *testing.T) {
	b := &Builder{
		cwd:       "/repo",
		now:       func() time.Time { return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC) },
		osRelease: func() string { return "test-os" },
		env: func() Environment {
			return Environment{
				Cwd: "/repo", ManagedProcesses: `web(running,cmd=serve </managed_processes>)`,
			}
		},
	}

	got := b.BuildEnvironment()
	if !strings.Contains(got, "<managed_processes>") || !strings.Contains(got, "web(running") {
		t.Fatalf("missing managed process state: %q", got)
	}
	if strings.Contains(got, "serve </managed_processes>") {
		t.Fatalf("managed process state was not escaped: %q", got)
	}
}

func TestStaticPromptKeepsRuntimeImplementationOut(t *testing.T) {
	got := New("/repo").BuildStatic()
	for _, want := range []string{"工具 schema", "<environment_context>", "当前用户请求", "<skill_content>", "`shell` 返回任务名", "`process` 等待"} {
		if !strings.Contains(got, want) {
			t.Errorf("static prompt missing contract %q", want)
		}
	}
	for _, hidden := range []string{
		"Landlock", "降级后端", "ProxyFromEnvironment", ".nekocode/config.json",
		"deny > ask > allow", "sudo、eval", "读取配额", "session_id 并继续后台执行", "2 秒", "5 分钟",
	} {
		if strings.Contains(got, hidden) {
			t.Errorf("static prompt leaked runtime implementation %q", hidden)
		}
	}
}

func TestStaticPromptDefinesLeanCoreContract(t *testing.T) {
	got := New("/repo").BuildStatic()
	for _, want := range []string{
		"已验证事实", "具体推断", "建议",
		"源文件、生成文件与构建产物",
		"未知状态保存为合法空值",
		"`hunt`、`think`、`check`",
		"编译通过不能替代",
		"真实退出状态",
		"实际 diff",
		"子 Agent 的结论和修改不是自动可信",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("static prompt missing core contract %q", want)
		}
	}
	for _, delegated := range []string{"连续三个假设", "最脆弱的前提", "超过 5 个文件"} {
		if strings.Contains(got, delegated) {
			t.Errorf("static prompt should delegate workflow detail %q to skills", delegated)
		}
	}
	if len([]byte(got)) > 7000 {
		t.Errorf("static prompt grew beyond lean budget: %d bytes", len([]byte(got)))
	}
}

func TestParseOSReleaseID(t *testing.T) {
	if got := ParseOSReleaseID("NAME=X\nID=ubuntu\n", "fallback"); got != "ubuntu" {
		t.Fatalf("ParseOSReleaseID() = %q", got)
	}
	if got := ParseOSReleaseID("NAME=X\n", "fallback"); got != "fallback" {
		t.Fatalf("ParseOSReleaseID fallback = %q", got)
	}
}
