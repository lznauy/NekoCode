package modelcatalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testAPI = `{"custom":{"api":"https://service.example/v1","models":{"new-model":{"id":"new-model","modalities":{"output":["text"]},"limit":{"context":96000,"input":80000,"output":16000}}}}}`

func fixture(t *testing.T) *Catalog {
	t.Helper()
	c, err := Decode([]byte(testAPI))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCatalogMatchesEndpointAndExactID(t *testing.T) {
	c := fixture(t)
	for _, endpoint := range []string{"https://service.example/v1", "https://service.example", "https://SERVICE.EXAMPLE/v1/"} {
		l, ok := c.Lookup(endpoint, "new-model")
		if !ok || l.Context != 96000 || l.Input != 80000 || l.Output != 16000 {
			t.Fatalf("lookup %s: %+v %v", endpoint, l, ok)
		}
	}
	for _, tc := range [][2]string{
		{"https://other.example/v1", "new-model"}, {"https://service.example.evil/v1", "new-model"},
		{"https://service.example/v1", "prefix/new-model"}, {"https://service.example/v1", "new-model-preview"},
		{"https://service.example/private/v1", "new-model"},
		{"https://service.example/v1/v1", "new-model"},
		{"https://service.example/%76%31", "new-model"}, {"https://user:secret@service.example/v1", "new-model"},
		{"https://service.example/v1?key=secret", "new-model"},
	} {
		if _, ok := c.Lookup(tc[0], tc[1]); ok {
			t.Fatalf("unexpected match: %v", tc)
		}
	}
}

func TestBundledCatalogAndRoundTrip(t *testing.T) {
	c := Bundled()
	if l, ok := c.Lookup("https://api.deepseek.com/v1", "deepseek-v4-flash"); !ok || l.Context <= 0 {
		t.Fatal("bundled catalog missing default model")
	}
	raw, err := c.MarshalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	clone, err := parseSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(clone.data.Endpoints) != len(c.data.Endpoints) {
		t.Fatal("snapshot round-trip lost endpoints")
	}
}

func TestInvalidCatalogCannotReplaceCache(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"version":1,"endpoints":{}}`, `{"version":1,"fetched_at":"2026-01-01T00:00:00Z","endpoints":{"https://service.example":{"x":{"context":-1}}}}`} {
		if _, err := parseSnapshot([]byte(raw)); err == nil {
			t.Fatalf("invalid snapshot accepted: %s", raw)
		}
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	old := fixture(t)
	old.data.FetchedAt = time.Now().Add(-48 * time.Hour)
	raw, _ := old.MarshalSnapshot()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("offline")
	if err := refreshCache(t.Context(), path, func(context.Context) (*Catalog, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || string(retained) != string(raw) {
		t.Fatal("failed update changed cache")
	}
}

func TestRefreshPinsOldSnapshotAndSkipsFreshDownloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "catalog.json")
	old := fixture(t)
	calls := 0
	download := func(context.Context) (*Catalog, error) { calls++; return fixture(t), nil }
	if err := refreshCache(t.Context(), path, download); err != nil {
		t.Fatal(err)
	}
	if err := refreshCache(t.Context(), path, download); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("fresh cache downloaded %d times", calls)
	}
	// A cache replacement changes future loads, never an existing pointer.
	updated := fixture(t)
	updated.data.Endpoints["https://service.example/v1"]["new-model"] = Limits{Context: 64000}
	updated.data.FetchedAt = time.Now().Add(-48 * time.Hour)
	raw, _ := updated.MarshalSnapshot()
	os.WriteFile(path, raw, 0600)
	if l, _ := old.Lookup("https://service.example/v1", "new-model"); l.Context != 96000 {
		t.Fatal("old catalog mutated")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := refreshCache(canceled, path, download); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	current, err := readCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := current.Lookup("https://service.example/v1", "new-model"); l.Context != 64000 {
		t.Fatal("canceled refresh replaced cache")
	}
}

func TestLoadFallbackAndOfflineSwitch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("NEKOCODE_MODEL_CATALOG_OFFLINE", "1")
	if err := RefreshCache(t.Context()); err != nil {
		t.Fatal(err)
	}
	if Load() != Bundled() {
		t.Fatal("missing cache did not use bundled snapshot")
	}
	os.MkdirAll(filepath.Dir(cachePath()), 0700)
	os.WriteFile(cachePath(), []byte("broken"), 0600)
	if Load() != Bundled() {
		t.Fatal("corrupt cache did not use bundled snapshot")
	}
}

func TestFetchUsesMetadataOnlyAndChecksResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			t.Error("request leaked client data")
		}
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Error("missing JSON accept header")
		}
		w.Write([]byte(testAPI))
	}))
	defer server.Close()
	c, err := fetch(t.Context(), server.Client(), server.URL+"/api.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup("https://service.example/v1", "new-model"); !ok {
		t.Fatal("fetched catalog unavailable")
	}
	if _, err := fetch(t.Context(), server.Client(), server.URL+"/fail"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatal(err)
	}
}

func TestLegacyCacheUsesBundledUntilSchemaRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := cachePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	old := fixture(t)
	old.data.Version = 1
	raw, _ := old.MarshalSnapshot()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if Load() != Bundled() {
		t.Fatal("old cache suppressed new bundled reasoning metadata")
	}
	calls := 0
	if err := refreshCache(t.Context(), path, func(context.Context) (*Catalog, error) { calls++; return fixture(t), nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("fresh legacy schema skipped migration")
	}
	restored, err := readCache(path)
	if err != nil || restored.data.Version != 3 {
		t.Fatalf("migration failed: %v", err)
	}
}
