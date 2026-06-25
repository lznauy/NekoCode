package main

import (
	"context"
	"embed"
	"log"

	"nekocode/bot/session"
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
