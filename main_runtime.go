package main

import controlruntime "nekocode/runtime"

func (a *App) MemoryView(scope string) controlruntime.MemoryView {
	return a.impl.MemoryView(scope)
}

func (a *App) InteractionMode() controlruntime.InteractionMode {
	return a.impl.InteractionMode()
}

func (a *App) SetInteractionMode(id string) (controlruntime.InteractionMode, error) {
	return a.impl.SetInteractionMode(id)
}

func (a *App) BackgroundTasks() ([]controlruntime.BackgroundTask, error) {
	return a.impl.BackgroundTasks()
}

func (a *App) AppendBackgroundTask(sessionID, id, text string) error {
	return a.impl.AppendBackgroundTask(sessionID, id, text)
}

func (a *App) CancelBackgroundTask(sessionID, id string) error {
	return a.impl.CancelBackgroundTask(sessionID, id)
}

func (a *App) StopBackgroundTasks(sessionID string) error {
	return a.impl.StopBackgroundTasks(sessionID)
}

func (a *App) SubmitBackgroundDemand(sessionID, text string) error {
	return a.impl.SubmitBackgroundDemand(sessionID, text)
}

func (a *App) BackgroundDemands(sessionID string) ([]controlruntime.BackgroundDemand, error) {
	return a.impl.BackgroundDemands(sessionID)
}

func (a *App) ResumeBackgroundDemand(sessionID, id string) error {
	return a.impl.ResumeBackgroundDemand(sessionID, id)
}

func (a *App) DiscardBackgroundDemand(sessionID, id string) error {
	return a.impl.DiscardBackgroundDemand(sessionID, id)
}

func (a *App) BackgroundUsage(sessionID string) (controlruntime.BackgroundUsage, error) {
	return a.impl.BackgroundUsage(sessionID)
}

func (a *App) ListTasks(sessionID string, all bool, offset, limit int) (controlruntime.SubagentPage, error) {
	return a.impl.ListTasks(sessionID, all, offset, limit)
}

func (a *App) TaskHistory(parent, id string, before, limit int) (controlruntime.SubagentHistoryPage, error) {
	return a.impl.TaskHistory(parent, id, before, limit)
}

func (a *App) TaskAttempts(parent, id string, before, limit int) (controlruntime.SubagentAttemptPage, error) {
	return a.impl.TaskAttempts(parent, id, before, limit)
}

func (a *App) TaskOutput(parent, id string, sequence int, offset int64) (controlruntime.SubagentOutputPage, error) {
	return a.impl.TaskOutput(parent, id, sequence, offset)
}

func (a *App) DeleteTask(parent, id string) error {
	return a.impl.DeleteTask(parent, id)
}

func (a *App) SearchTaskOutputs(parent, id, query string, after, limit int) (controlruntime.TaskSearchPage, error) {
	return a.impl.SearchTaskOutputs(parent, id, query, after, limit)
}

func (a *App) RetryTaskReview(parent, id string) error {
	return a.impl.RetryTaskReview(parent, id)
}

func (a *App) ExportTaskOutputs(parent, id string) (string, error) {
	return a.impl.ExportTaskOutputs(parent, id)
}
