// Package calllog writes one privacy-safe structured JSONL record per LLM call.
// Records contain session identity, usage, latency, provider routing, and prefix
// diagnostics but never prompt text, request bodies, headers, or credentials.
package calllog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	providertypes "nekocode/bot/provider/types"
	"nekocode/util/fs"
	utilhttp "nekocode/util/http"
	"nekocode/util/text"
)

const (
	logFileName = "llm-calls.jsonl"
	maxSize     = 10 << 20
)

// PrefixDiag is the local view of the request's cache-relevant shape: which
// parts changed since the previous call and the fingerprints of the stable
// prefix. Hashes are 12 hex chars — enough to eyeball equality in a log.
type PrefixDiag struct {
	ChangedParts []string `json:"changed_parts,omitempty"`
	SystemHash   string   `json:"system_hash,omitempty"`
	ToolsHash    string   `json:"tools_hash,omitempty"`
	HistoryCount int      `json:"history_count,omitempty"`
	HistoryHash  string   `json:"history_hash,omitempty"`
}

// IsZero reports whether no diagnostic field was populated.
func (d PrefixDiag) IsZero() bool {
	return len(d.ChangedParts) == 0 && d.SystemHash == "" && d.ToolsHash == "" &&
		d.HistoryCount == 0 && d.HistoryHash == ""
}

// Record is one LLM call. CacheUsageReported distinguishes a provider's real
// zero values from calls where cache details were unavailable.
type Record struct {
	TS                 time.Time `json:"ts"`
	Seq                uint64    `json:"seq"`
	Source             string    `json:"source"`
	SessionID          string    `json:"session,omitempty"`
	Model              string    `json:"model,omitempty"`
	Protocol           string    `json:"protocol,omitempty"`
	BaseURL            string    `json:"base_url,omitempty"`
	RequestedEffort    string    `json:"requested_effort,omitempty"`
	EffectiveEffort    string    `json:"effective_effort,omitempty"`
	DurMs              int64     `json:"dur_ms"`
	TTFTMs             int64     `json:"ttft_ms,omitempty"`
	TotalTokens        int       `json:"total"`
	InTokens           int       `json:"in"`
	CachedTokens       int       `json:"cached"`
	NewTokens          int       `json:"new"`
	OutTokens          int       `json:"out"`
	ReasoningTokens    int       `json:"reasoning,omitempty"`
	CacheUsageReported bool      `json:"cache_usage_reported"`
	CacheHitRatio      *float64  `json:"cache_hit_ratio,omitempty"`
	Usage              string    `json:"usage,omitempty"`
	// SystemFingerprint is a short hash of the provider's serving-cluster id;
	// hit/miss correlated with hash changes points at cache routing without
	// persisting a provider-controlled string.
	SystemFingerprint string      `json:"system_fingerprint,omitempty"`
	PrefixDiag        *PrefixDiag `json:"prefix,omitempty"`
	Err               string      `json:"err,omitempty"`
}

// SetUsage normalizes provider usage and stores both machine-readable numbers
// and the compact human summary used when inspecting the JSONL file.
func (r *Record) SetUsage(usage providertypes.StreamUsage) {
	usage.Normalize()
	input := max(0, usage.PromptTokens)
	r.InTokens = input
	r.OutTokens = max(0, usage.CompletionTokens)
	r.TotalTokens = r.InTokens + r.OutTokens
	r.ReasoningTokens = max(0, min(usage.ReasoningTokens, r.OutTokens))
	r.CacheUsageReported = usage.CacheUsageReported
	if input == 0 && r.OutTokens == 0 {
		return
	}
	if !usage.CacheUsageReported {
		r.Usage = FormatUsage(r.InTokens, 0, 0, r.OutTokens, r.ReasoningTokens, false)
		return
	}
	r.CachedTokens = max(0, min(usage.CacheHitTokens, input))
	r.NewTokens = max(0, usage.CacheMissTokens)
	if input > 0 {
		ratio := float64(r.CachedTokens) / float64(input)
		r.CacheHitRatio = &ratio
	}
	r.Usage = FormatUsage(r.InTokens, r.CachedTokens, r.NewTokens, r.OutTokens, r.ReasoningTokens, true)
}

// FormatUsage renders one LLM call as an absolute cache split. Absolute token
// counts distinguish a healthy growing prefix from an actual cache collapse.
func FormatUsage(input, cached, fresh, output, reasoning int, cacheReported bool) string {
	input = max(0, input)
	output = max(0, output)
	cached = max(0, cached)
	fresh = max(0, fresh)
	reasoning = max(0, min(reasoning, output))
	parts := []string{text.FormatTokens(input+output) + " tok", "in " + text.FormatTokens(input)}
	if cacheReported {
		parts = append(parts, "cached "+text.FormatTokens(cached), "new "+text.FormatTokens(fresh))
	} else {
		parts = append(parts, "cached ?", "new ?")
	}
	parts = append(parts, "out "+text.FormatTokens(output))
	if reasoning > 0 {
		parts = append(parts, "reasoning "+text.FormatTokens(reasoning))
	}
	return strings.Join(parts, " · ")
}

// FingerprintID preserves equality comparisons without persisting the raw,
// provider-controlled fingerprint value.
func FingerprintID(value string) string {
	if value == "" {
		return ""
	}
	return ShortDigest(sha256.Sum256([]byte(value)))
}

// SafeBaseURL retains only the endpoint origin. Credentials, path components,
// query parameters, and fragments are removed before anything is written.
func SafeBaseURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.User = nil
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}

// ErrorSummary returns a diagnostic category without persisting provider
// response bodies, request URLs, or other values embedded in error strings.
func ErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "context canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context deadline exceeded"
	}
	var httpErr *utilhttp.HTTPError
	if errors.As(err, &httpErr) {
		return fmt.Sprintf("API error (HTTP %d)", httpErr.StatusCode)
	}
	t := reflect.TypeOf(err)
	if t == nil {
		return "error"
	}
	return "error: " + t.String()
}

var (
	seq         atomic.Uint64
	defaultSink = &sink{path: filepath.Join(fs.NekocodeLogDir(), logFileName)}
)

type sink struct {
	mu   sync.Mutex
	path string
	file *os.File
}

// Write appends one record to the JSONL call log. Best-effort: logging must
// never break an LLM call.
func Write(rec Record) {
	rec.Seq = seq.Add(1)
	if rec.TS.IsZero() {
		rec.TS = time.Now()
	}
	defaultSink.write(rec)
}

func (s *sink) write(rec Record) {
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.logFile()
	if f == nil {
		return
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		s.file = nil
	}
}

func (s *sink) logFile() *os.File {
	if s.file != nil {
		return s.file
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return nil
	}
	if fi, err := os.Stat(s.path); err == nil && fi.Size() > maxSize {
		rotated := s.path + ".1"
		_ = os.Remove(rotated)
		if err := os.Rename(s.path, rotated); err == nil {
			_ = os.Chmod(rotated, 0o600)
		}
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	s.file = f
	return s.file
}

// ShortDigest renders a 32-byte digest as 12 hex chars for PrefixDiag.
func ShortDigest(digest [32]byte) string {
	return hex.EncodeToString(digest[:6])
}
