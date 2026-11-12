package app

import (
	"context"
	"fmt"
	"nekocode/mode"
	"strings"

	controlruntime "nekocode/runtime"
)

type backgroundController interface {
	CurrentSessionID() string
	BackgroundTasks(string) ([]controlruntime.BackgroundTask, error)
	AppendBackgroundTask(context.Context, string, string, string) error
	CancelBackgroundTask(context.Context, string, string) error
}

func (a *App) BackgroundTasks() ([]controlruntime.BackgroundTask, error) {
	rt, ok := a.rt.(backgroundController)
	if !ok {
		return nil, fmt.Errorf("background tasks unavailable")
	}
	return rt.BackgroundTasks(rt.CurrentSessionID())
}

func (a *App) AppendBackgroundTask(sessionID, id, text string) error {
	rt, ok := a.rt.(backgroundController)
	if !ok {
		return fmt.Errorf("background tasks unavailable")
	}
	return rt.AppendBackgroundTask(context.Background(), sessionID, id, text)
}

func (a *App) CancelBackgroundTask(sessionID, id string) error {
	rt, ok := a.rt.(backgroundController)
	if !ok {
		return fmt.Errorf("background tasks unavailable")
	}
	return rt.CancelBackgroundTask(context.Background(), sessionID, id)
}

func (a *App) StopBackgroundTasks(sessionID string) error {
	rt, ok := a.rt.(interface {
		StopBackgroundTasks(context.Context, string) error
	})
	if !ok {
		return fmt.Errorf("background tasks unavailable")
	}
	return rt.StopBackgroundTasks(context.Background(), sessionID)
}

// SubmitBackgroundDemand uses acceptance events to create the chat turn when
// admitted, so a queued demand cannot steal the current assistant's buffers.
func (a *App) SubmitBackgroundDemand(sessionID, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("empty demand")
	}
	if a.InteractionMode().Selected != mode.SuperAgent {
		return fmt.Errorf("new background demands require Super Agent mode")
	}
	rt, ok := a.rt.(interface {
		SubmitBackgroundDemand(context.Context, string, controlruntime.Input) (controlruntime.RunID, error)
	})
	if !ok {
		return fmt.Errorf("background demands unavailable")
	}
	_, err := rt.SubmitBackgroundDemand(context.Background(), sessionID, controlruntime.Input{Text: text, Source: controlruntime.SourceRef{Kind: "gui_queue"}})
	return err
}

func (a *App) BackgroundDemands(sessionID string) ([]controlruntime.BackgroundDemand, error) {
	rt, ok := a.rt.(interface {
		BackgroundDemands(string) ([]controlruntime.BackgroundDemand, error)
	})
	if !ok {
		return nil, fmt.Errorf("demand inbox unavailable")
	}
	return rt.BackgroundDemands(sessionID)
}
func (a *App) ResumeBackgroundDemand(sessionID, id string) error {
	rt, ok := a.rt.(interface {
		ResumeBackgroundDemand(context.Context, string, controlruntime.RunID) error
	})
	if !ok {
		return fmt.Errorf("demand inbox unavailable")
	}
	return rt.ResumeBackgroundDemand(context.Background(), sessionID, controlruntime.RunID(id))
}
func (a *App) DiscardBackgroundDemand(sessionID, id string) error {
	rt, ok := a.rt.(interface {
		DiscardBackgroundDemand(context.Context, string, controlruntime.RunID) error
	})
	if !ok {
		return fmt.Errorf("demand inbox unavailable")
	}
	return rt.DiscardBackgroundDemand(context.Background(), sessionID, controlruntime.RunID(id))
}

func (a *App) BackgroundUsage(sessionID string) (controlruntime.BackgroundUsage, error) {
	rt, ok := a.rt.(interface {
		BackgroundUsage(string) (controlruntime.BackgroundUsage, error)
	})
	if !ok {
		return controlruntime.BackgroundUsage{}, fmt.Errorf("background usage unavailable")
	}
	return rt.BackgroundUsage(sessionID)
}
