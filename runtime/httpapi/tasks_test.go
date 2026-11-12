package httpapi

import (
	"context"
	"fmt"
	"io"
	controlruntime "nekocode/runtime"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskManagementHTTPRoutes(t *testing.T) {
	var deleted, exported bool
	services := controlruntime.ServiceGroups{Sessions: controlruntime.SessionsServices{
		CurrentSessionID: func() string { return "parent" }, ListSessions: func() []controlruntime.SessionMeta { return []controlruntime.SessionMeta{{ID: "parent"}} }, SessionMessages: func() []controlruntime.DisplayMessage { return nil },
		Subagents: func(parent string) ([]controlruntime.SubagentSummary, error) {
			return []controlruntime.SubagentSummary{{ID: "child", SessionID: parent, Status: "completed"}}, nil
		},
		DeleteSubagent: func(parent, id string) error {
			if parent != "parent" || id != "child" {
				return fmt.Errorf("wrong identity")
			}
			deleted = true
			return nil
		},
		SearchSubagentOutputs: func(_ context.Context, parent, id, query string, after, limit int) (controlruntime.TaskSearchPage, error) {
			if parent != "parent" || id != "child" || query != "needle" || after != 2 || limit != 3 {
				return controlruntime.TaskSearchPage{}, fmt.Errorf("wrong query")
			}
			return controlruntime.TaskSearchPage{Hits: []controlruntime.TaskSearchHit{{Sequence: 3, Preview: "needle"}}}, nil
		},
		ExportSubagentOutputs: func(_ context.Context, parent, id string, w io.Writer) error {
			if parent != "parent" || id != "child" {
				return fmt.Errorf("wrong task")
			}
			exported = true
			_, err := io.WriteString(w, "full report")
			return err
		},
	}}
	rt := controlruntime.New(controlruntime.RunnerFunc(func(context.Context, string, controlruntime.RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	handler := New(rt).Handler()
	request := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	for _, path := range []string{"/tasks?all=1&limit=1", "/tasks/child/search?session_id=parent&query=needle&after=2&limit=3", "/background?session_id=parent", "/demands?session_id=parent"} {
		rec := request("GET", path)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	rec := request("GET", "/tasks/child/export?session_id=parent")
	if rec.Code != 200 || rec.Body.String() != "full report" || !exported {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	rec = request("GET", "/tasks/other/export?session_id=parent")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "wrong task") {
		t.Fatalf("invalid export reported success: %d %s", rec.Code, rec.Body.String())
	}
	if rec = request("DELETE", "/tasks/child"); rec.Code != 400 || deleted {
		t.Fatal("delete without session identity accepted")
	}
	if rec = request("DELETE", "/tasks/child?session_id=parent"); rec.Code != 200 || !deleted {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}
