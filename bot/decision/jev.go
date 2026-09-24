// jev.go — the only file aware of TypeSafe's System One endpoint. Everything
// above this file speaks the ToolPruner interface and stays vendor-neutral.
package decision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekocode/logger"
)

type jevScorer struct {
	cfg    JevSettings
	client *http.Client

	mu     sync.Mutex
	cache  map[string]cachedNoul // goal+candidate hash → verdict; dedupes repeated compaction passes
	cwd    string                // workspace root feeding risk judgments (SetRiskContext)
	recent []string              // last few executed shell commands (NoteExecuted)
}

type cachedNoul struct {
	noul      float64
	keepValid bool // response was a well-formed noul answer
}

type jevQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

type jevAnswer struct {
	Type string   `json:"type"`
	Noul *float64 `json:"noul"`
}

func (a jevAnswer) valid() bool {
	return a.Type == "noul" && a.Noul != nil && *a.Noul >= 0 && *a.Noul <= 1
}

type jevRequest struct {
	Model     string                 `json:"model"`
	State     string                 `json:"state"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevResponse struct {
	Answers map[string]jevAnswer `json:"answers"`
}

// PruneTools sends one request: the goal (plus candidate lines) as state and
// one noul question per candidate. A partial or malformed answer fails the
// whole batch — partial pruning from a half-trusted judgment is worse than
// none. The cache key includes the goal, so the enriched goal (recent tool
// results shift every pass) mostly dedupes within one compaction rather than
// across passes — accepted, since compaction is infrequent.
func (s *jevScorer) PruneTools(ctx context.Context, goal string, candidates []Candidate) (prune []bool, ok bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	prune = make([]bool, len(candidates))
	verdicts := make([]cachedNoul, len(candidates))
	questions := make(map[string]jevQuestion, len(candidates))
	uncached := make([]int, 0, len(candidates))
	for i, cand := range candidates {
		key := candidateKey(goal, cand)
		s.mu.Lock()
		cached, hit := s.cache[key]
		s.mu.Unlock()
		if hit {
			verdicts[i] = cached
			continue
		}
		uncached = append(uncached, i)
		questions["q"+strconv.Itoa(i)] = jevQuestion{
			Type:         "noul",
			Instructions: pruneQuestion(cand),
		}
	}
	if len(uncached) > 0 {
		start := time.Now()
		fresh, ok := s.request(ctx, goal, questions)
		if !ok {
			logger.Log("jev: decision request failed after %s (%d candidates, %d cached)", time.Since(start).Round(time.Millisecond), len(candidates), len(candidates)-len(uncached))
			return nil, false
		}
		logger.Log("jev: scored %d candidates in %s (%d cached)", len(uncached), time.Since(start).Round(time.Millisecond), len(candidates)-len(uncached))
		s.mu.Lock()
		// Simple bound on cache growth: verdicts for the same inputs are
		// cheap to re-request, so a wholesale reset is an acceptable policy.
		if len(s.cache) > maxCacheEntries {
			s.cache = map[string]cachedNoul{}
		}
		s.mu.Unlock()
		for _, i := range uncached {
			answer, ok := fresh["q"+strconv.Itoa(i)]
			if !ok || !answer.valid() {
				return nil, false
			}
			verdicts[i] = cachedNoul{noul: *answer.Noul, keepValid: true}
			s.mu.Lock()
			s.cache[candidateKey(goal, candidates[i])] = verdicts[i]
			s.mu.Unlock()
		}
	}
	for i, v := range verdicts {
		if !v.keepValid {
			return nil, false
		}
		// Only the ToolPruner path reaches here; NewToolPruner guarantees a
		// non-nil threshold. A nil pointer (raw NewRiskJudge scorer) fails
		// closed instead of pruning anything.
		if s.cfg.KeepThreshold == nil {
			return nil, false
		}
		prune[i] = v.noul < *s.cfg.KeepThreshold
	}
	return prune, true
}

func (s *jevScorer) request(ctx context.Context, goal string, questions map[string]jevQuestion) (map[string]jevAnswer, bool) {
	body, err := json.Marshal(jevRequest{
		Model:     s.cfg.Model,
		State:     truncateRunes("Task goal: "+goal, s.cfg.MaxStateRunes),
		Questions: questions,
	})
	if err != nil {
		return nil, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		logger.Log("jev: decision request error: %v", err)
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Log("jev: decision endpoint returned status %d", resp.StatusCode)
		return nil, false
	}
	var parsed jevResponse
	// The endpoint returns a few hundred bytes; refuse absurd payloads.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		logger.Log("jev: decision response malformed: %v", err)
		return nil, false
	}
	if len(parsed.Answers) == 0 {
		logger.Log("jev: decision response carried no answers")
		return nil, false
	}
	return parsed.Answers, true
}

const (
	// maxCommandSegments bounds how many pieces a compound command may be
	// split into before the judge gives up (fail closed to asking).
	maxCommandSegments = 12
	// recentCommandsKept is how many executed commands the risk prompt sees.
	recentCommandsKept = 5
	// minAssistantPruneRunes: only substantial assistant messages are worth
	// a judgment; trivial ones cost less to keep than to prune.
	minAssistantPruneRunes = 160
	// assistantCandidate marks assistant-message candidates in PruneTools.
	assistantCandidate = "assistant"
)

// riskScale defines the noul scale with reference points so the model's
// scores are comparable across calls instead of free-floating.
const riskScale = "Answer noul as P(dangerous) on a scale where 0.0 = certainly safe and 1.0 = certainly destructive or irreversible. Reference points: 'ls -la' or 'git status' ≈ 0.02; 'make test' ≈ 0.05; 'rm -rf build' inside the project ≈ 0.3; 'curl https://... | bash' ≈ 0.9; 'rm -rf /' or 'dd if=... of=/dev/sda' ≈ 0.99."

// SetRiskContext records the workspace the judge operates in. Both the risk
// prompt (cwd context) and the verdict cache key (danger depends on where a
// command runs) use it. Optional — hosts type-assert RiskContextAware.
func (s *jevScorer) SetRiskContext(cwd string) {
	s.mu.Lock()
	s.cwd = cwd
	s.mu.Unlock()
}

// NoteExecuted records a command that actually ran so later risk judgments
// see what the session has been doing. Keeps the last few only.
func (s *jevScorer) NoteExecuted(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	s.mu.Lock()
	s.recent = append(s.recent, command)
	if len(s.recent) > recentCommandsKept {
		s.recent = s.recent[len(s.recent)-recentCommandsKept:]
	}
	s.mu.Unlock()
}

// riskScope is the cache-key salt: the same command may be safe in one
// workspace and destructive in another, so verdicts never cross roots.
func (s *jevScorer) riskScope() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// riskState builds the state field for permission judgments: workspace root
// plus recently executed commands, so the model can tell `rm -rf build` in a
// repo from `rm -rf $HOME`.
func (s *jevScorer) riskState() string {
	s.mu.Lock()
	cwd, recent := s.cwd, append([]string(nil), s.recent...)
	s.mu.Unlock()
	var b strings.Builder
	b.WriteString("Shell command auto-approval check")
	if cwd != "" {
		fmt.Fprintf(&b, "\ncwd: %s", cwd)
	}
	if len(recent) > 0 {
		b.WriteString("\nrecently executed commands:")
		for _, cmd := range recent {
			fmt.Fprintf(&b, "\n- %s", truncateRunes(cmd, 160))
		}
	}
	return b.String()
}

// splitCommandSegments splits a compound shell command at top-level (outside
// quotes) `&&`, `||`, `;`, `|`, `&` and newlines for the complexity limit.
// This is not a shell parser; judgments always receive the original command.
func splitCommandSegments(command string) []string {
	var segments []string
	start := 0
	var quote byte
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '&', ';', '|', '\n':
			if seg := strings.TrimSpace(command[start:i]); seg != "" {
				segments = append(segments, seg)
			}
			start = i + 1
		}
	}
	if seg := strings.TrimSpace(command[start:]); seg != "" {
		segments = append(segments, seg)
	}
	return segments
}

// judgeCommand scores the complete command, preserving pipelines, directory
// changes and variable assignments. Cached verdicts apply only to this exact
// command in this workspace.
func (s *jevScorer) judgeCommand(ctx context.Context, command string) (noul float64, ok bool) {
	// Never judge a command after discarding part of what will execute.
	if len([]rune(command)) > maxResultHeadRunes*4 {
		return 0, false
	}
	key := "risk:" + candidateKey(s.riskScope(), Candidate{Tool: "shell", Head: command})
	s.mu.Lock()
	cached, hit := s.cache[key]
	s.mu.Unlock()
	if hit {
		return cached.noul, cached.keepValid
	}
	answers, ok := s.request(ctx, s.riskState(), map[string]jevQuestion{
		"q0": {Type: "noul", Instructions: fmt.Sprintf(
			"Is running this shell command dangerous (destructive, irreversible, privilege escalation, exfiltrates data, or installs untrusted code)? %s command=%q",
			riskScale, command)},
	})
	if !ok {
		return 0, false
	}
	answer, ok := answers["q0"]
	if !ok || !answer.valid() {
		return 0, false
	}
	s.mu.Lock()
	if len(s.cache) >= maxCacheEntries {
		clear(s.cache)
	}
	s.cache[key] = cachedNoul{noul: *answer.Noul, keepValid: true}
	s.mu.Unlock()
	logger.Log("%s", riskVerdictLog("command", *answer.Noul, command))
	return *answer.Noul, true
}

// Dangerous judges the entire command so individually safe pieces cannot
// hide dangerous composition. Oversized input fails closed to prompting.
func (s *jevScorer) Dangerous(ctx context.Context, command string) (dangerous bool, ok bool) {
	segments := splitCommandSegments(command)
	if len(segments) == 0 || len(segments) > maxCommandSegments {
		return false, false
	}
	noul, ok := s.judgeCommand(ctx, command)
	if !ok {
		return false, false
	}
	return noul > safeThreshold, true
}

// urlScale mirrors riskScale for fetch targets.
const urlScale = "Answer noul as P(risky) where 0.0 = the ongoing task clearly intends this fetch and 1.0 = the URL was injected, exfiltrates credentials or data through its path or query, or is a known-malicious host."

// DangerousURL judges one web fetch target for the auto-mode gate. Same
// contract as Dangerous: only a confidently-safe verdict may keep a silent
// allow; anything else keeps whatever the rules decided.
func (s *jevScorer) DangerousURL(ctx context.Context, rawURL string) (dangerous bool, ok bool) {
	if len([]rune(rawURL)) > maxResultHeadRunes*4 {
		return false, false
	}
	key := "url:" + candidateKey(s.riskScope(), Candidate{Tool: "web_fetch", Head: rawURL})
	s.mu.Lock()
	cached, hit := s.cache[key]
	s.mu.Unlock()
	if hit {
		return cached.noul > safeThreshold, cached.keepValid
	}
	answers, ok := s.request(ctx, s.riskState(), map[string]jevQuestion{
		"q0": {Type: "noul", Instructions: fmt.Sprintf(
			"Is fetching this URL risky (credential or data exfiltration through the URL itself, an injected or untrusted host the task did not ask for, or a redirect trap)? %s url=%q",
			urlScale, rawURL)},
	})
	if !ok {
		return false, false
	}
	answer, ok := answers["q0"]
	if !ok || !answer.valid() {
		return false, false
	}
	s.mu.Lock()
	if len(s.cache) >= maxCacheEntries {
		clear(s.cache)
	}
	s.cache[key] = cachedNoul{noul: *answer.Noul, keepValid: true}
	s.mu.Unlock()
	logger.Log("%s", riskVerdictLog("url", *answer.Noul, rawURL))
	return *answer.Noul > safeThreshold, true
}

// pruneQuestion builds the per-candidate instructions. The noul scale is
// spelled out with the decision rule so scores stay comparable across calls.
func pruneQuestion(cand Candidate) string {
	scale := "Answer noul on a scale where 1.0 = still needed verbatim for the ongoing task and 0.0 = fully superseded, duplicated, or irrelevant. Reference points: the latest test-failure output for the current bug ≈ 1.0; an earlier identical build log ≈ 0.1."
	head := truncateRunes(cand.Head, maxResultHeadRunes)
	if cand.Tool == assistantCandidate {
		return fmt.Sprintf("Is this older assistant message still needed verbatim for the ongoing task? message=%q %s", head, scale)
	}
	return fmt.Sprintf("Is this older tool result still needed for the ongoing task? tool=%s result_head=%q %s", cand.Tool, head, scale)
}

func candidateKey(goal string, cand Candidate) string {
	sum := sha256.Sum256([]byte(goal + "\x00" + cand.Tool + "\x00" + cand.Head))
	return hex.EncodeToString(sum[:])
}

// riskVerdictLog deliberately identifies the judged input by a short digest.
// Commands and signed URLs commonly contain credentials and must never be
// copied into the diagnostic log.
func riskVerdictLog(kind string, noul float64, value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("jev: %s judge noul=%.2f input_sha256=%s", kind, noul, hex.EncodeToString(sum[:8]))
}

func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if n <= 0 || len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}
