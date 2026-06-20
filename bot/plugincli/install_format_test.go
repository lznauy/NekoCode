package plugincli

import (
	"strings"
	"testing"

	"nekocode/bot/extension/plugin"
)

func TestFormatInstallPreview(t *testing.T) {
	p := &plugin.Plugin{
		Manifest: plugin.Manifest{
			Name:        "demo-plugin",
			Version:     "1.2.3",
			Description: "demo description",
			Skills:      []string{"skills"},
			Agents:      []string{"agents/reviewer.md"},
		},
		Dir:            "/tmp/demo-plugin",
		HasInstallStub: true,
	}

	out := FormatInstallPreview(p)
	for _, want := range []string{"demo-plugin", "v1.2.3", "demo description", "Skills: 1", "Agents: 1", "[!] install.sh detected"} {
		if !strings.Contains(out, want) {
			t.Fatalf("preview = %q, missing %q", out, want)
		}
	}
}

func TestFormatInstallResult(t *testing.T) {
	p := &plugin.Plugin{
		Manifest: plugin.Manifest{
			Name:    "demo-plugin",
			Version: "1.2.3",
			Skills:  []string{"skills"},
		},
		Dir:            "/tmp/demo-plugin",
		HasInstallStub: true,
	}

	out := FormatInstallResult(p)
	for _, want := range []string{"Installed plugin", "demo-plugin", "install.sh", "Skills: 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result = %q, missing %q", out, want)
		}
	}
}
