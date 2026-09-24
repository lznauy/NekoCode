// Package decision hosts optional, fail-open "System One" judgments
// (currently TypeSafe Jev) that enhance hot paths — context compaction,
// tool-output trimming — without coupling them to any vendor. A nil scorer
// means the capability is absent and every caller keeps its existing
// behavior; a scorer failure must degrade to that same behavior.
package decision

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

// Candidate is one item the host wants judged.
type Candidate struct {
	Tool string // tool name, e.g. "shell"
	Head string // head of the item content for the decision input
}

// ToolPruner scores stale tool results before compaction summarizes them.
// Implementations must fail open: PruneTools returns ok=false whenever the
// judgment is unavailable or untrustworthy, and the caller then keeps the
// original content.
type ToolPruner interface {
	// PruneTools returns one prune flag per candidate, evaluated in parallel
	// against the same goal state. true means the candidate lost relevance
	// and may be replaced by a placeholder.
	PruneTools(ctx context.Context, goal string, candidates []Candidate) (prune []bool, ok bool)
}

// JevSettings configures the Jev-backed scorer. Zero values fall back to
// defaults, so a config file only needs the API key.
type JevSettings struct {
	APIKey        string        // required; also read from TYPESAFE_API_KEY when empty
	Model         string        // default jev-latest; pin jev-1.x for stable thresholds
	BaseURL       string        // default https://api.typesafe.ai/v1/systemone
	KeepThreshold *float64      // noul below this prunes the result; nil uses default 0.2, explicit 0 disables pruning
	Enabled       *bool         // master switch; nil/true enables, explicit false disables every Jev capability
	Timeout       time.Duration // per request; default 3s
	MaxStateRunes int           // goal truncation; default 4000
}

const (
	DefaultJevModel      = "jev-latest"
	DefaultJevBaseURL    = "https://api.typesafe.ai/v1/systemone"
	defaultKeepThreshold = 0.2
	defaultTimeout       = 3 * time.Second
	defaultMaxStateRunes = 4000
	maxResultHeadRunes   = 240
	maxCacheEntries      = 512
	// safeThreshold is the risk-judge gate: a shell command is auto-approved
	// only when P(dangerous) ≤ 0.2. Deliberately NOT the compaction
	// keep_threshold — the two knobs must not silently affect each other.
	safeThreshold = 0.2
)

// RiskJudge is the permission-layer capability: a calibrated "is this shell
// command dangerous?" check. It never approves — callers may only *skip a
// prompt* when Dangerous returns false with ok=true, and must fall back to
// prompting when ok=false.
type RiskJudge interface {
	Dangerous(ctx context.Context, command string) (dangerous bool, ok bool)
}

// URLRiskJudge extends the permission gate to web fetch targets: an auto-mode
// host may consult it before silently fetching a URL the rules allow. Same
// contract as RiskJudge — never approves, only may force a prompt.
type URLRiskJudge interface {
	DangerousURL(ctx context.Context, url string) (dangerous bool, ok bool)
}

// RiskContextAware lets the host feed execution context (workspace root,
// recently executed commands) into the judge. Optional enrichment: hosts
// type-assert and skip when the judge does not support it.
type RiskContextAware interface {
	SetRiskContext(cwd string)
	NoteExecuted(command string)
}

// NewRiskJudge builds the Jev-backed RiskJudge, or nil when no API key is
// available. Mirrors NewToolPruner's "not configured, no impact" contract.
func NewRiskJudge(settings JevSettings) RiskJudge {
	s := newScorer(settings)
	if s == nil {
		return nil
	}
	return s
}

// NewToolPruner builds the Jev-backed ToolPruner, or nil when no API key is
// available (neither settings nor TYPESAFE_API_KEY) — the "not configured,
// no impact" entry point.
func NewToolPruner(settings JevSettings) ToolPruner {
	s := newScorer(settings)
	if s == nil {
		return nil
	}
	if s.cfg.KeepThreshold == nil {
		threshold := defaultKeepThreshold
		s.cfg.KeepThreshold = &threshold
	}
	return s
}

// newScorer applies key presence and the shared defaults; nil means the
// capability is not configured. The master switch (Enabled=false) wins over
// key presence so a configured engine can be turned off without deleting the
// key or the environment variable.
func newScorer(settings JevSettings) *jevScorer {
	if settings.Enabled != nil && !*settings.Enabled {
		return nil
	}
	key := settings.APIKey
	if key == "" {
		key = os.Getenv("TYPESAFE_API_KEY")
	}
	if strings.TrimSpace(key) == "" {
		return nil
	}
	settings.APIKey = strings.TrimSpace(key)
	if settings.Model == "" {
		settings.Model = DefaultJevModel
	}
	if settings.BaseURL == "" {
		settings.BaseURL = DefaultJevBaseURL
	}
	if settings.Timeout <= 0 {
		settings.Timeout = defaultTimeout
	}
	if settings.MaxStateRunes <= 0 {
		settings.MaxStateRunes = defaultMaxStateRunes
	}
	return &jevScorer{cfg: settings, client: &http.Client{Timeout: settings.Timeout}, cache: map[string]cachedNoul{}}
}
