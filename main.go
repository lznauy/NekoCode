package main

import (
	"context"
	"embed"
	"log"

	botconfig "nekocode/bot/config"
	"nekocode/bot/session"
	botskill "nekocode/bot/skill"
	"nekocode/common"
	"nekocode/guiapp"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:gui/dist
var assets embed.FS

// App keeps the Wails binding package stable as go/main/App while delegating
// the GUI implementation to guiapp.
type App struct {
	impl *guiapp.App
}

func NewApp() *App {
	return &App{impl: guiapp.NewApp()}
}

func (a *App) Startup(ctx context.Context) {
	a.impl.Startup(ctx)
}

func (a *App) Shutdown(ctx context.Context) {
	a.impl.Shutdown(ctx)
}

func (a *App) DomReady(ctx context.Context) {
	a.impl.DomReady(ctx)
}

func (a *App) SendMessage(input string) {
	a.impl.SendMessage(input)
}

func (a *App) Abort() {
	a.impl.Abort()
}

func (a *App) ProviderModel() string {
	return a.impl.ProviderModel()
}

func (a *App) GetConfig() botconfig.Snapshot {
	return a.impl.GetConfig()
}

func (a *App) SaveConfig(cfg botconfig.Snapshot) (botconfig.Snapshot, error) {
	return a.impl.SaveConfig(cfg)
}

func (a *App) GetSkillManagement() botskill.ManagementSnapshot {
	return a.impl.GetSkillManagement()
}

func (a *App) RefreshSkillManagement() botskill.ManagementSnapshot {
	return a.impl.RefreshSkillManagement()
}

func (a *App) SetPluginEnabled(name string, enabled bool) (botskill.ManagementSnapshot, error) {
	return a.impl.SetPluginEnabled(name, enabled)
}

func (a *App) ListSessions() []session.Meta {
	return a.impl.ListSessions()
}

func (a *App) NewSession() (session.Meta, error) {
	return a.impl.NewSession()
}

func (a *App) LoadSession(id string) ([]common.DisplayMessage, error) {
	return a.impl.LoadSession(id)
}

func (a *App) DeleteSession(id string) error {
	return a.impl.DeleteSession(id)
}

func (a *App) ReadImageBase64(path string) (string, error) {
	return a.impl.ReadImageBase64(path)
}

func (a *App) ReplyConfirm(id string, ok bool) {
	a.impl.ReplyConfirm(id, ok)
}

func (a *App) ReplyQuestion(id string, answersJSON string, rejected bool) {
	a.impl.ReplyQuestion(id, answersJSON, rejected)
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "NekoCode",
		Width:     960,
		Height:    720,
		MinWidth:  480,
		MinHeight: 360,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		OnDomReady: app.DomReady,
		Bind:       []any{app},
	})

	if err != nil {
		log.Fatal(err)
	}
}
