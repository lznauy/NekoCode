package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestBodyLimit(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		rt := newFakeRuntime()
		req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"`+strings.Repeat("x", 8<<20)+`"}`))
		if chunked {
			req.ContentLength = -1
		}
		rec := httptest.NewRecorder()
		New(rt).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("chunked=%v status=%d", chunked, rec.Code)
		}
		if rt.lastInput.Text != "" {
			t.Fatal("oversized request reached runtime")
		}
	}
}

func TestRequestBodyLimitAtBoundaryAndTrailingData(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"exact limit", `{"text":"` + strings.Repeat("x", (1<<20)-11) + `"}`, 202},
		{"trailing whitespace", `{"text":"hello"}` + strings.Repeat(" ", 1<<20), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := newFakeRuntime()
			req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(tc.body))
			req.ContentLength = -1
			rec := httptest.NewRecorder()
			New(rt).Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d", rec.Code, tc.want)
			}
			if tc.want == 413 && rt.lastInput.Text != "" {
				t.Fatal("oversize body mutated runtime")
			}
		})
	}
}

type unreadableBody struct{ t *testing.T }

func (b unreadableBody) Read([]byte) (int, error) {
	b.t.Fatal("body limit middleware read an observation request body")
	return 0, nil
}
func (unreadableBody) Close() error { return nil }

func TestBodyLimitDoesNotReadObservationBodies(t *testing.T) {
	rt := newFakeRuntime()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Body = unreadableBody{t}
	req.ContentLength = 1
	rec := httptest.NewRecorder()
	New(rt).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}
