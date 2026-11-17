package a2aapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	rt "nekocode/runtime"
)

func TestTerminalTaskIncludesSessionCleanupWarning(t *testing.T) {
	var mu sync.Mutex
	current, created := "main", false
	runtime := rt.New(rt.RunnerFunc(func(context.Context, string, rt.RunHost) (string, error) { return "answer", nil }), rt.ServiceGroups{Sessions: rt.SessionsServices{
		CurrentSessionID: func() string { mu.Lock(); defer mu.Unlock(); return current },
		ListSessions:     func() []rt.SessionMeta { return nil }, SessionMessages: func() []rt.DisplayMessage { return nil },
		NewSession: func() (rt.SessionMeta, error) {
			mu.Lock()
			defer mu.Unlock()
			if created {
				return rt.SessionMeta{}, errors.New("fallback unavailable")
			}
			created, current = true, "a2a"
			return rt.SessionMeta{ID: current}, nil
		},
		ResumeSession: func(string) error { return errors.New("main session unavailable") },
	}})
	t.Cleanup(func() { _ = runtime.Close() })
	server, err := New(runtime, Options{Endpoint: "http://localhost/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work"))})
	if err != nil {
		t.Fatal(err)
	}
	response := serveA2ARequest(t, server, http.MethodPost, "/message:send", body)
	var result struct {
		Task *a2a.Task `json:"task"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if runtime.Status().State != rt.RuntimeBusy {
		t.Fatal("test did not trigger failed cleanup")
	}
	if result.Task == nil || result.Task.Status.Message == nil {
		t.Fatalf("cleanup error hidden: %s", response.Body.String())
	}
	text := result.Task.Status.Message.Parts[0].Text()
	if !strings.Contains(text, "session restoration") {
		t.Fatalf("empty cleanup warning: %s", response.Body.String())
	}
}

func TestTerminalConsumerMayStopAtAnyEvent(t *testing.T) {
	for limit := 1; limit <= 5; limit++ {
		r := &fakeRuntime{eventBatches: [][]rt.Event{{
			{Sequence: 1, Type: rt.EventRunDone, Payload: rt.RunResult{Output: "answer"}},
			{Sequence: 2, Type: rt.EventSystemMessage, Payload: rt.MessagePayload{Content: "cleanup warning"}},
		}}}
		e := newExecutor(r)
		count := 0
		for _, err := range e.Execute(context.Background(), &a2asrv.ExecutorContext{TaskID: "task", ContextID: "context", Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work"))}) {
			if err != nil {
				t.Fatal(err)
			}
			count++
			if count == limit {
				break
			}
		}
	}
}
