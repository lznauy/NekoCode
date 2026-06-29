package app

import (
	"fmt"

	"nekocode/bot/app/pluginops"
	"nekocode/bot/debug"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/plugincli"
)

func (b *Bot) pluginInstall(args []string) (string, bool) {
	parsed := pluginops.ParseInstallArgs(args)
	if !parsed.OK {
		return pluginops.InstallUsage, true
	}
	source := parsed.Source

	if plugincli.IsLocalPath(source) {
		return b.pluginInstallLocal(source, parsed.Confirmed)
	}
	if !parsed.Confirmed {
		b.setPendingConfirmation(true)
		go b.fetchAndConfirmRemote(source)
		return fmt.Sprintf("Fetching plugin info from %s ...", source), true
	}

	go b.pluginInstallAsync(source)
	return fmt.Sprintf("Installing from %s ...", source), true
}

func (b *Bot) pluginInstallLocal(source string, confirmed bool) (string, bool) {
	p, err := b.pluginReg.PreviewFromPath(source)
	if err != nil {
		return fmt.Sprintf("Preview failed: %v", err), true
	}
	if confirmed {
		return b.pluginInstallSync(source)
	}

	b.setPendingConfirmation(true)
	go func() {
		if b.confirmInstall(source, p, false) {
			result, _ := b.pluginInstallSync(source)
			if b.notifyFn != nil {
				b.notifyFn(result)
			}
		}
	}()
	return plugincli.FormatInstallPreview(p), true
}

func (b *Bot) fetchAndConfirmRemote(source string) {
	p, err := pluginops.FetchRemotePreview(source, plugincli.FetchURL)
	if err != nil {
		if b.notifyFn != nil {
			b.notifyFn(fmt.Sprintf("%v\n\n/plugin install %s --yes  to skip preview.", err, source))
		}
		b.unblockConfirm()
		return
	}
	if b.confirmInstall(source, p, true) {
		b.pluginInstallAsync(source)
	}
}

func (b *Bot) pluginInstallSync(source string) (string, bool) {
	p, err := b.pluginReg.Install(source)
	if err != nil {
		return pluginops.InstallFailed(err), true
	}
	return b.registerPluginExtensions(p)
}

func (b *Bot) pluginInstallAsync(source string) {
	p, err := b.pluginReg.Install(source)
	if err != nil {
		if b.notifyFn != nil {
			b.notifyFn(pluginops.InstallFailed(err))
		}
		return
	}
	result, _ := b.registerPluginExtensions(p)
	if b.notifyFn != nil {
		b.notifyFn(result)
	}
}

func (b *Bot) registerPluginExtensions(p *plugin.Plugin) (string, bool) {
	for _, d := range p.SkillDirs() {
		if err := b.skillReg.Load([]string{d}); err != nil {
			debug.Log("plugin: skill load error: %v", err)
		}
	}
	b.refreshSkillList()
	b.loadPluginExtensions(p)
	return plugincli.FormatInstallResult(p), true
}
