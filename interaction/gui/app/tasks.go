package app

import (
	"context"
	"fmt"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"io"
	controlruntime "nekocode/runtime"
	"os"
	"path/filepath"
)

type taskManager interface {
	Subagents(controlruntime.SubagentFilter) (controlruntime.SubagentPage, error)
	SubagentHistory(string, string, int, int) (controlruntime.SubagentHistoryPage, error)
	SubagentAttempts(string, string, int, int) (controlruntime.SubagentAttemptPage, error)
	SubagentToolOutput(string, string, int, int64) (controlruntime.SubagentOutputPage, error)
	DeleteSubagent(context.Context, string, string) error
	SearchSubagentOutputs(context.Context, string, string, string, int, int) (controlruntime.TaskSearchPage, error)
	ExportSubagentOutputs(context.Context, string, string, io.Writer) error
	RetryBackgroundNotification(context.Context, string, string) error
}

func (a *App) ListTasks(sessionID string, all bool, offset, limit int) (controlruntime.SubagentPage, error) {
	rt, ok := a.rt.(taskManager)
	if !ok {
		return controlruntime.SubagentPage{}, fmt.Errorf("task management unavailable")
	}
	return rt.Subagents(controlruntime.SubagentFilter{SessionID: sessionID, AllSessions: all, Offset: offset, Limit: limit})
}
func (a *App) TaskHistory(parent, id string, before, limit int) (controlruntime.SubagentHistoryPage, error) {
	rt, ok := a.rt.(taskManager)
	if !ok {
		return controlruntime.SubagentHistoryPage{}, fmt.Errorf("task history unavailable")
	}
	return rt.SubagentHistory(parent, id, before, limit)
}
func (a *App) TaskAttempts(parent, id string, before, limit int) (controlruntime.SubagentAttemptPage, error) {
	rt, ok := a.rt.(taskManager)
	if !ok {
		return controlruntime.SubagentAttemptPage{}, fmt.Errorf("task attempts unavailable")
	}
	return rt.SubagentAttempts(parent, id, before, limit)
}
func (a *App) TaskOutput(parent, id string, sequence int, offset int64) (controlruntime.SubagentOutputPage, error) {
	rt, ok := a.rt.(taskManager)
	if !ok {
		return controlruntime.SubagentOutputPage{}, fmt.Errorf("task output unavailable")
	}
	return rt.SubagentToolOutput(parent, id, sequence, offset)
}
func (a *App) DeleteTask(parent, id string) error {
	rt, ok := a.rt.(taskManager)
	if !ok {
		return fmt.Errorf("task management unavailable")
	}
	return rt.DeleteSubagent(context.Background(), parent, id)
}
func (a *App) SearchTaskOutputs(parent, id, query string, after, limit int) (controlruntime.TaskSearchPage, error) {
	rt, ok := a.rt.(taskManager)
	if !ok {
		return controlruntime.TaskSearchPage{}, fmt.Errorf("task management unavailable")
	}
	return rt.SearchSubagentOutputs(context.Background(), parent, id, query, after, limit)
}
func (a *App) RetryTaskReview(parent, id string) error {
	rt, ok := a.rt.(taskManager)
	if !ok {
		return fmt.Errorf("task management unavailable")
	}
	return rt.RetryBackgroundNotification(context.Background(), parent, id)
}
func (a *App) ExportTaskOutputs(parent, id string) (string, error) {
	rt, ok := a.rt.(taskManager)
	if !ok {
		return "", fmt.Errorf("task export unavailable")
	}
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{Title: "导出任务输出", DefaultFilename: "task-output.txt"})
	if err != nil || path == "" {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".task-export-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	err = rt.ExportSubagentOutputs(context.Background(), parent, id, f)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	return path, err
}
