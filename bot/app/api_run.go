package app

import (
	"nekocode/bot/command"
	"nekocode/common"
)

func (b *Bot) Run(input string, callbacks common.RunCallbacks) (string, error) {
	if callbacks.Text != nil || callbacks.Reason != nil {
		b.SetCallbacks(callbacks.Text, callbacks.Reason)
	}
	return b.RunAgent(input, callbacks.Step)
}

func (b *Bot) RunAgent(input string, onStep func(action, toolName, toolArgs, output string)) (string, error) {
	ag := b.getAgent()
	result := ag.Run(input, onStep)
	ag.SetPlanMode(false)
	b.ctxMgr.SetSystemPrompt(b.promptBuilder.Build())
	command.SummarizeIfNeeded(b.ctxMgr)
	b.sess.Save()
	return result.FinalOutput, result.Error
}
