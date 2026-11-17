package lspcore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticsRequiresCurrentPublishedResult(t *testing.T) {
	for _, state := range []string{"missing", "stale", "fresh"} {
		t.Run(state, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "main.go")
			if err := os.WriteFile(path, []byte("package main\n"), 0600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			uri := pathToURI(path)
			c := &client{docs: map[string]*docState{uri: {version: 2, size: info.Size(), mod: info.ModTime()}}, diags: map[string][]Diagnostic{}, diagVer: map[string]int{}}
			if state != "missing" {
				c.diags[uri] = nil
				c.diagVer[uri] = 1
			}
			if state == "fresh" {
				c.diagVer[uri] = 2
			}
			m := NewManager(dir, DefaultSpecs())
			m.clients["go"] = c
			// No subprocess was started; only release the manager context.
			defer m.cancel()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			out, err := m.Diagnostics(ctx, path)
			if state == "fresh" {
				if err != nil || !strings.Contains(out, "no problems found") {
					t.Fatalf("fresh empty result: %q %v", out, err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(out, "no problems found") {
				t.Fatalf("unconfirmed diagnostics reported as success: %q %v", out, err)
			}
		})
	}
}

func TestWaitDiagnosticsTimeoutAndCancellation(t *testing.T) {
	c := &client{diags: map[string][]Diagnostic{}, diagVer: map[string]int{}}
	if _, err := c.waitDiagnostics(t.Context(), "file:///missing.go", 1, time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("missing diagnostics must report unknown: %v", err)
	}
	c.diagVer["file:///fresh.go"] = 1
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.waitDiagnostics(ctx, "file:///fresh.go", 1, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation masked by cached diagnostics: %v", err)
	}
}

func TestDiagnosticsPreservesUnversionedFreshnessUncertainty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	c := &client{docs: map[string]*docState{uri: {version: 2, size: info.Size(), mod: info.ModTime()}}, diags: map[string][]Diagnostic{}, diagVer: map[string]int{}}
	m := NewManager(dir, DefaultSpecs())
	m.clients["go"] = c
	defer m.cancel()
	for _, diagnostics := range []string{"[]", `[{"severity":1,"message":"unverified error"}]`} {
		payload := json.RawMessage(`{"uri":` + strconv.Quote(uri) + `,"diagnostics":` + diagnostics + `}`)
		c.handleNotify("textDocument/publishDiagnostics", payload)
		out, err := m.Diagnostics(t.Context(), path)
		if err != nil || !strings.Contains(out, "freshness is unverified") || strings.Contains(out, "no problems found") {
			t.Fatalf("versionless publication confirmed current file: %q %v", out, err)
		}
		if diagnostics != "[]" && !strings.Contains(out, "unverified error") {
			t.Fatalf("versionless findings were discarded: %q", out)
		}
	}
	// A later explicitly versioned publication restores verified freshness.
	c.handleNotify("textDocument/publishDiagnostics", json.RawMessage(`{"uri":`+strconv.Quote(uri)+`,"version":2,"diagnostics":[]}`))
	out, err := m.Diagnostics(t.Context(), path)
	if err != nil || !strings.Contains(out, "no problems found") || strings.Contains(out, "unverified") {
		t.Fatalf("versioned publication did not restore verification: %q %v", out, err)
	}
}
