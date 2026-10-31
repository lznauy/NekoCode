package main

import controlruntime "nekocode/runtime"

// Keep transport methods on the Wails-bound wrapper, not only on the inner
// GUI implementation. Wails generates frontend bindings from main.App.
var _ interface {
	MCPAuthorizationAction(string, string) error
	Abort() error
	ClearSelectedSkill() error
	RefreshSkillManagement() (controlruntime.SkillManagementView, error)
	ReplyConfirm(string, bool) error
	ReplyConfirmDecision(string, bool, bool) error
	ReplyQuestion(string, string, bool) error
	CommandMenu(string) *controlruntime.CommandMenu
	ResolveModelProfile(controlruntime.ModelSpec) controlruntime.ModelProfile
} = (*App)(nil)
