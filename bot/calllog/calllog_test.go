package calllog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
	utilhttp "nekocode/util/http"
)

func TestRecordSetUsage(t *testing.T) {
	var rec Record
	rec.SetUsage(types.StreamUsage{PromptTokens: 21_865, CacheHitTokens: 21_200, CompletionTokens: 732, ReasoningTokens: 283, CacheUsageReported: true})
	if rec.TotalTokens != 22_597 || rec.InTokens != 21_865 || rec.CachedTokens != 21_200 || rec.NewTokens != 665 || rec.OutTokens != 732 || rec.ReasoningTokens != 283 {
		t.Fatalf("normalized usage = %+v", rec)
	}
	if rec.CacheHitRatio == nil || *rec.CacheHitRatio < 0.96 || rec.Usage != "22.6k tok · in 21.9k · cached 21.2k · new 665 · out 732 · reasoning 283" {
		t.Fatalf("usage analysis = %+v", rec)
	}
	var unknown Record
	unknown.SetUsage(types.StreamUsage{PromptTokens: 100, CompletionTokens: 10})
	if unknown.CacheUsageReported || unknown.CacheHitRatio != nil || unknown.Usage != "110 tok · in 100 · cached ? · new ? · out 10" {
		t.Fatalf("unknown cache usage = %+v", unknown)
	}
}

func TestPrivacySafeDiagnostics(t *testing.T) {
	if got := FingerprintID("fp-cluster-secret"); got == "" || strings.Contains(got, "secret") {
		t.Fatalf("fingerprint id leaked raw value: %q", got)
	}
	err := utilhttp.NewHTTPError(400, `{"error":"prompt contained sk-secret"}`)
	if got := ErrorSummary(err); got != "API error (HTTP 400)" {
		t.Fatalf("HTTP error summary = %q", got)
	}
	if got := ErrorSummary(context.Canceled); got != "context canceled" {
		t.Fatalf("cancellation summary = %q", got)
	}
	if got := ErrorSummary(errors.New("user prompt secret")); got != "error: *errors.errorString" {
		t.Fatalf("generic error summary = %q", got)
	}
	if got := SafeBaseURL("https://user:secret@example.com/v1/secret?api_key=secret#token"); got != "https://example.com" {
		t.Fatalf("safe base URL = %q", got)
	}
}

func TestSinkWriteAppendsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	s := &sink{path: path}

	s.write(Record{Source: "main", Model: "m1", NewTokens: 10})
	s.write(Record{Source: "subagent", Err: "boom"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	var first, second Record
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 not JSON: %v", err)
	}
	if first.Source != "main" || first.Model != "m1" || first.NewTokens != 10 {
		t.Errorf("record 1 = %+v", first)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("line 2 not JSON: %v", err)
	}
	if second.Err != "boom" {
		t.Errorf("record 2 = %+v", second)
	}
}

func TestShortDigest(t *testing.T) {
	if got := ShortDigest([32]byte{0xab, 0xcd, 0xef, 1, 2, 3}); got != "abcdef010203" {
		t.Fatalf("ShortDigest = %q", got)
	}
}

// captureWrite redirects the package sink and registers cleanup so a test can
// assert on real Write output without touching the user's log directory.
func captureWrite(t *testing.T) *sink {
	t.Helper()
	s := &sink{path: filepath.Join(t.TempDir(), "calls.jsonl")}
	original := defaultSink
	defaultSink = s
	t.Cleanup(func() {
		defaultSink = original
	})
	return s
}

func readRecords(t *testing.T, path string) []Record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var records []Record
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d not JSON: %v", i+1, err)
		}
		records = append(records, rec)
	}
	return records
}

func TestWriteKeepsExplicitSessionID(t *testing.T) {
	s := captureWrite(t)

	Write(Record{Source: "main", SessionID: "explicit"})

	records := readRecords(t, s.path)
	if len(records) != 1 || records[0].SessionID != "explicit" {
		t.Fatalf("records = %+v", records)
	}
}

func TestWriteOmitsSessionWithoutProvider(t *testing.T) {
	s := captureWrite(t)

	Write(Record{Source: "main"})

	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(data), `"session"`) {
		t.Fatalf("record should omit session when no provider is registered: %s", data)
	}
}
