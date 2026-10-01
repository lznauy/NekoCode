package main

import (
	"context"
	"embed"
	"log"

	"nekocode/interaction/gui/app"
	controlruntime "nekocode/runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:interaction/gui/web/dist
var assets embed.FS

// App keeps the Wails binding package stable as go/main/App while delegating
// the GUI implementation to the interaction GUI app package.
type App struct {
	impl *app.App
}

func NewApp() (*App, error) {
	impl, err := app.NewApp()
	if err != nil {
		return nil, err
	}
	return &App{impl: impl}, nil
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

func (a *App) SendMessage(input string, images []controlruntime.ImageAttachment) error {
	return a.impl.SendMessage(input, images)
}

func (a *App) SaveClipboardImage(dataURL string) (string, error) {
	return a.impl.SaveClipboardImage(dataURL)
}

func (a *App) DeleteClipboardImage(path string) error {
	return a.impl.DeleteClipboardImage(path)
}

func (a *App) CommandMenu(input string) *controlruntime.CommandMenu {
	return a.impl.CommandMenu(input)
}

func (a *App) Abort() {
	a.impl.Abort()
}

func (a *App) CurrentModel() string {
	return a.impl.CurrentModel()
}

func (a *App) SwitchModel(name string) (string, error) {
	return a.impl.SwitchModel(name)
}

func (a *App) ContextSnapshot() controlruntime.ContextSnapshot {
	return a.impl.ContextSnapshot()
}

func (a *App) SelectSkill(name string) error {
	return a.impl.SelectSkill(name)
}

func (a *App) ClearSelectedSkill() {
	a.impl.ClearSelectedSkill()
}

func (a *App) GetConfig() controlruntime.ConfigView {
	return a.impl.GetConfig()
}

func (a *App) ResolveModelProfile(model controlruntime.ModelSpec) controlruntime.ModelProfile {
	return a.impl.ResolveModelProfile(model)
}

func (a *App) SaveConfig(cfg controlruntime.ConfigView) (controlruntime.ConfigView, error) {
	return a.impl.SaveConfig(cfg)
}

func (a *App) GetSkillManagement() controlruntime.SkillManagementView {
	return a.impl.GetSkillManagement()
}

func (a *App) RefreshSkillManagement() controlruntime.SkillManagementView {
	return a.impl.RefreshSkillManagement()
}

func (a *App) SetPluginEnabled(name string, enabled bool) (controlruntime.SkillManagementView, error) {
	return a.impl.SetPluginEnabled(name, enabled)
}

func (a *App) ListSessions() []controlruntime.SessionMeta {
	return a.impl.ListSessions()
}

func (a *App) NewSession() (controlruntime.SessionMeta, error) {
	return a.impl.NewSession()
}

func (a *App) LoadSession(id string) ([]controlruntime.DisplayMessage, error) {
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

func (a *App) ReplyConfirmDecision(id string, ok bool, remember bool) {
	a.impl.ReplyConfirmDecision(id, ok, remember)
}

func (a *App) ReplyQuestion(id string, answersJSON string, rejected bool) {
	a.impl.ReplyQuestion(id, answersJSON, rejected)
}

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatal(err)
	}

	err = wails.Run(&options.App{
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
