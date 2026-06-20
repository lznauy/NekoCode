package bot

import "nekocode/bot/app"

type Bot = app.Bot

func New() *Bot {
	return app.New()
}
