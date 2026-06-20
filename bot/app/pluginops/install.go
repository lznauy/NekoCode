package pluginops

import (
	"fmt"

	"nekocode/bot/extension/plugin"
	"nekocode/bot/plugincli"
)

const InstallUsage = "Usage: /plugin install <source>\n  source: GitHub URL | user/repo | ./local-path"

type InstallArgs struct {
	Source    string
	Confirmed bool
	OK        bool
}

func ParseInstallArgs(args []string) InstallArgs {
	if len(args) == 0 {
		return InstallArgs{}
	}
	return InstallArgs{
		Source:    args[0],
		Confirmed: len(args) >= 2 && args[1] == "--yes",
		OK:        true,
	}
}

func FetchRemotePreview(source string, fetch func(string) ([]byte, error)) (*plugin.Plugin, error) {
	rawURL := plugincli.SourceToRawURL(source)
	if rawURL == "" {
		return nil, fmt.Errorf("fetch plugin info: preview URL not available for %s", source)
	}
	data, err := fetch(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch plugin info: %w", err)
	}
	m, err := plugin.ParseManifestData(data)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}
	return &plugin.Plugin{Manifest: *m, Dir: "", Source: source}, nil
}

func ConfirmSummary(p *plugin.Plugin, isRemote bool) string {
	summary := plugincli.FormatInstallPreview(p)
	if isRemote {
		summary += "\n(install.sh will not be executed automatically)"
	}
	return summary
}
