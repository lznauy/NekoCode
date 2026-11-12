package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nekocode/mode"
	controlruntime "nekocode/runtime"
)

func TestModeAPIValidatesAndRespectsRunBoundary(t *testing.T) {
	selected := mode.Standard
	release := make(chan struct{})
	rt := controlruntime.New(controlruntime.RunnerFunc(func(context.Context, string, controlruntime.RunHost) (string, error) { <-release; return "", nil }), controlruntime.ServiceGroups{Modes: controlruntime.ModesServices{
		Current: func() mode.ID { return selected }, Options: mode.Definitions,
		Set: func(id mode.ID) error {
			d, err := mode.Resolve(id)
			if err != nil {
				return err
			}
			selected = d.ID
			return nil
		},
		Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(selected); return d.Snapshot, err },
	}})
	defer rt.Close()
	handler := New(rt).Handler()
	request := func(method, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, "/mode", strings.NewReader(body)))
		return rec
	}
	if rec := request(http.MethodGet, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"selected":"standard"`) {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request(http.MethodPost, `{"mode":"super-agent"}`); rec.Code != 200 || selected != mode.SuperAgent {
		t.Fatalf("Super Agent POST: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request(http.MethodPost, `{"mode":"plan"}`); rec.Code != 200 || selected != mode.Plan {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	for _, body := range []string{`{}`, `{"mode":"unknown-mode"}`, `{"mode":"plan"} {}`} {
		if rec := request(http.MethodPost, body); rec.Code != 400 || selected != mode.Plan {
			t.Fatalf("invalid %s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	id, err := rt.StartRun(context.Background(), controlruntime.Input{Text: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(http.MethodPost, `{"mode":"standard"}`); rec.Code != http.StatusConflict || selected != mode.Plan {
		t.Fatalf("busy: %d %s", rec.Code, rec.Body.String())
	}
	close(release)
	if err := rt.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}
