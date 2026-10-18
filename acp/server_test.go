package acp

import (
	"context"
	"io"
	"strings"
	"testing"

	controlruntime "nekocode/runtime"
)

type fakeBackend struct {
	current    string
	sessions   []controlruntime.SessionMeta
	events     chan controlruntime.Event
	eventCtx   context.Context
	input      controlruntime.Input
	deleted    string
	cancelled  controlruntime.RunID
	waited     bool
	mcpAdded   []string
	mcpCleared int

	models        []controlruntime.ModelOption
	activeModel   string
	currentEffort string
	switchedModel string
	effortSet     string
	fullAccess    bool
	metrics       controlruntime.MetricsSnapshot

	newSessionErr error
	resumeErr     error
	replaceMCPErr error
}

func (f *fakeBackend) StartRun(_ context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	f.input = input
	if f.events != nil {
		f.events <- controlruntime.Event{RunID: "run-1", Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: "done"}}
		f.events <- controlruntime.Event{RunID: "run-1", Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "done"}}
	}
	return "run-1", nil
}
func (f *fakeBackend) WaitRun(context.Context, controlruntime.RunID) error {
	f.waited = true
	return nil
}
func (f *fakeBackend) CancelRun(_ context.Context, id controlruntime.RunID) error {
	f.cancelled = id
	return nil
}
func (f *fakeBackend) DecideApproval(context.Context, string, controlruntime.ApprovalDecision) error {
	return nil
}
func (f *fakeBackend) AnswerQuestion(context.Context, string, controlruntime.QuestionReply) error {
	return nil
}
func (f *fakeBackend) Events(ctx context.Context, _ controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	f.eventCtx = ctx
	if f.events != nil {
		return f.events, nil
	}
	return make(chan controlruntime.Event), nil
}
func (f *fakeBackend) CurrentSessionID() string { return f.current }
func (f *fakeBackend) ListSessions() []controlruntime.SessionMeta {
	return append([]controlruntime.SessionMeta(nil), f.sessions...)
}
func (f *fakeBackend) SessionMessages() []controlruntime.DisplayMessage { return nil }
func (f *fakeBackend) NewSession() (controlruntime.SessionMeta, error) {
	if f.newSessionErr != nil {
		return controlruntime.SessionMeta{}, f.newSessionErr
	}
	item := controlruntime.SessionMeta{ID: "session-1", CWD: "/workspace"}
	f.current = item.ID
	f.sessions = append(f.sessions, item)
	return item, nil
}
func (f *fakeBackend) ResumeSession(id string) error {
	if f.resumeErr != nil {
		return f.resumeErr
	}
	f.current = id
	return nil
}
func (f *fakeBackend) DeleteSession(id string) error { f.deleted = id; return nil }
func (f *fakeBackend) ReplaceMCPServers(_ context.Context, source string, servers []controlruntime.MCPServerSpec) error {
	if f.replaceMCPErr != nil {
		return f.replaceMCPErr
	}
	if len(servers) == 0 {
		f.mcpCleared++
	}
	for _, server := range servers {
		f.mcpAdded = append(f.mcpAdded, source+":"+server.Name)
	}
	return nil
}

func (f *fakeBackend) ModelOptions() ([]controlruntime.ModelOption, string) {
	return f.models, f.activeModel
}
func (f *fakeBackend) CurrentModel() controlruntime.ModelSelection {
	return controlruntime.ModelSelection{Model: f.activeModel, ReasoningEffort: f.currentEffort}
}
func (f *fakeBackend) SwitchSessionModel(name string) (controlruntime.ModelSelection, error) {
	f.switchedModel = name
	f.activeModel = name
	return f.CurrentModel(), nil
}
func (f *fakeBackend) SetSessionReasoning(effort string) error {
	f.effortSet = effort
	f.currentEffort = effort
	for i := range f.models {
		if f.models[i].Name == f.activeModel {
			f.models[i].ReasoningEffort = effort
		}
	}
	return nil
}
func (f *fakeBackend) SetFullAccess(on bool) error {
	f.fullAccess = on
	return nil
}
func (f *fakeBackend) PermissionMode() string {
	if f.fullAccess {
		return "full"
	}
	return "manual"
}
func (f *fakeBackend) Metrics() controlruntime.MetricsSnapshot { return f.metrics }

func (f *fakeBackend) ApplySessionSettings(settings controlruntime.SessionSettings) error {
	if settings.Model != "" {
		if _, err := f.SwitchSessionModel(settings.Model); err != nil {
			return err
		}
	}
	if err := f.SetSessionReasoning(settings.ReasoningEffort); err != nil {
		return err
	}
	return f.SetFullAccess(settings.FullAccess)
}

func TestServeRejectsNilBackend(t *testing.T) {
	err := Serve(context.Background(), strings.NewReader(""), io.Discard, nil, "/workspace")
	if err == nil {
		t.Fatal("nil backend was accepted")
	}
}
