package app

import "nekocode/bot/agent/subagent"

func (b *Bot) registerPluginAgentPath(path string) error {
	def, err := subagent.ParseAgentMD(path)
	if err != nil {
		return err
	}
	subagent.RegisterPlugin(def.ToAgentType())
	return nil
}

func (b *Bot) unregisterPluginAgentPath(path string) {
	def, err := subagent.ParseAgentMD(path)
	if err == nil {
		subagent.UnregisterPlugin(def.Name)
	}
}
