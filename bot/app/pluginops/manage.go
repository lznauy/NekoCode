package pluginops

import (
	"fmt"

	"nekocode/bot/extension/plugin"
)

type LookupResult struct {
	Plugin  *plugin.Plugin
	Message string
	OK      bool
}

func RequirePlugin(args []string, lookup func(string) (*plugin.Plugin, bool), usage string) LookupResult {
	if len(args) == 0 {
		return LookupResult{Message: usage}
	}
	p, ok := lookup(args[0])
	if !ok {
		return LookupResult{Message: fmt.Sprintf("Plugin %q not found.", args[0])}
	}
	return LookupResult{Plugin: p, OK: true}
}

func AlreadyEnabled(name string) string {
	return fmt.Sprintf("Plugin %q is already enabled.", name)
}

func AlreadyDisabled(name string) string {
	return fmt.Sprintf("Plugin %q is already disabled.", name)
}

func Enabled(name string) string {
	return fmt.Sprintf("Enabled plugin %q.", name)
}

func Disabled(name string) string {
	return fmt.Sprintf("Disabled plugin %q.", name)
}

func Uninstalled(name string) string {
	return fmt.Sprintf("Uninstalled plugin %q.", name)
}

func InstallFailed(err error) string {
	return fmt.Sprintf("Install failed: %v", err)
}

func UninstallFailed(err error) string {
	return fmt.Sprintf("Uninstall failed: %v", err)
}

func EnableFailed(err error) string {
	return fmt.Sprintf("Enable failed: %v", err)
}

func DisableFailed(err error) string {
	return fmt.Sprintf("Disable failed: %v", err)
}
