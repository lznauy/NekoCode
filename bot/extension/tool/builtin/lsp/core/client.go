package lspcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// docState tracks what we last sent the server for a document, so ensureSynced
// can detect an out-of-band disk edit (any tool, including bash) by stat alone.
type docState struct {
	version int
	size    int64
	mod     time.Time
}

type client struct {
	cmd    *exec.Cmd
	conn   *conn
	root   string
	langID string
	posEnc string

	mu      sync.Mutex
	docs    map[string]*docState
	diags   map[string][]Diagnostic
	diagVer map[string]int
	// Unversioned pushes retain the local document state at receipt for cache
	// invalidation, but cannot prove which source version the server analyzed.
	diagUnversioned map[string]bool
	settled         bool // workspace index has settled at least once (see referencesSettled)
}

// Diagnostic is one published problem for a document.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

func startClient(ctx context.Context, bin string, args []string, env map[string]string, langID, root string) (*client, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), envSlice(env)...)
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &client{
		cmd:     cmd,
		root:    root,
		langID:  langID,
		docs:    map[string]*docState{},
		diags:   map[string][]Diagnostic{},
		diagVer: map[string]int{},
	}
	c.conn = newConn(stdin, stdout, c.handleNotify, c.handleRequest)
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.initialize(initCtx); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *client) initialize(ctx context.Context) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   pathToURI(c.root),
		// workspaceFolders scopes the server to the whole workspace. Without it
		// pyright and rust-analyzer treat only the opened file's directory as the
		// project, so cross-file queries (references, diagnostics) come back empty.
		"workspaceFolders": []map[string]any{
			{"uri": pathToURI(c.root), "name": filepath.Base(c.root)},
		},
		"capabilities": map[string]any{
			"general": map[string]any{
				"positionEncodings": []string{encodingUTF8, encodingUTF16},
			},
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{"versionSupport": true},
				"hover":              map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
			},
		},
	}
	res, err := c.conn.call(ctx, "initialize", params)
	if err != nil {
		return err
	}
	var r struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	_ = json.Unmarshal(res, &r)
	c.posEnc = r.Capabilities.PositionEncoding
	if c.posEnc == "" {
		c.posEnc = encodingUTF16
	}
	return c.conn.notify("initialized", map[string]any{})
}

// handleNotify caches diagnostics. The version guards waitDiagnostics against
// returning problems computed for pre-edit content.
func (c *client) handleNotify(method string, params json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string       `json:"uri"`
		Version     *int         `json:"version"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	c.mu.Lock()
	c.diags[p.URI] = p.Diagnostics
	if c.diagUnversioned == nil {
		c.diagUnversioned = map[string]bool{}
	}
	c.diagUnversioned[p.URI] = p.Version == nil
	if p.Version != nil {
		c.diagVer[p.URI] = *p.Version
	} else if d := c.docs[p.URI]; d != nil {
		c.diagVer[p.URI] = d.version
	}
	c.mu.Unlock()
}

// handleRequest answers the server→client requests that block initialization on
// some servers (rust-analyzer stalls without a workspace/configuration reply).
func (c *client) handleRequest(id int64, method string, params json.RawMessage) {
	switch method {
	case "workspace/configuration":
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(params, &p)
		_ = c.conn.reply(id, make([]any, len(p.Items)))
	default:
		_ = c.conn.reply(id, nil)
	}
}

func (c *client) ensureSynced(uri, path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	c.mu.Lock()
	d, open := c.docs[uri]
	c.mu.Unlock()
	if open && fi.Size() == d.size && fi.ModTime().Equal(d.mod) {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !open {
		err = c.conn.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": c.langID, "version": 1, "text": string(content),
			},
		})
		c.mu.Lock()
		c.docs[uri] = &docState{version: 1, size: fi.Size(), mod: fi.ModTime()}
		c.mu.Unlock()
		return err
	}
	ver := d.version + 1
	err = c.conn.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": ver},
		"contentChanges": []any{map[string]any{"text": string(content)}},
	})
	c.mu.Lock()
	c.docs[uri] = &docState{version: ver, size: fi.Size(), mod: fi.ModTime()}
	c.mu.Unlock()
	return err
}

func (c *client) docVersion(uri string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.docs[uri]; d != nil {
		return d.version
	}
	return 0
}

type diagnosticResult struct {
	items     []Diagnostic
	versioned bool
}

// waitDiagnostics waits for a matching publication. An unversioned server's
// result remains usable, but its freshness must not be presented as verified.
func (c *client) waitDiagnostics(ctx context.Context, uri string, minVer int, deadline time.Duration) (diagnosticResult, error) {
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return diagnosticResult{}, err
		}
		c.mu.Lock()
		ver, published := c.diagVer[uri]
		result := diagnosticResult{items: append([]Diagnostic(nil), c.diags[uri]...), versioned: !c.diagUnversioned[uri]}
		c.mu.Unlock()
		if published && ver >= minVer {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return diagnosticResult{}, ctx.Err()
		case <-timer.C:
			return diagnosticResult{}, fmt.Errorf("language server did not publish current diagnostics before the observation deadline; file status is unknown, not clean: %w", context.DeadlineExceeded)
		case <-tick.C:
		}
	}
}

// callRetry retries a request while the server answers ContentModified (-32801),
// which means it is mid-reindex and the state is in flux. A short bounded retry
// hides the brief window after a didOpen/didChange; a longer reindex still
// surfaces so the caller can decide (see Manager, which turns it into a
// retry-shortly message).
func (c *client) callRetry(ctx context.Context, method string, params any) (json.RawMessage, error) {
	const attempts = 5
	for i := 0; ; i++ {
		raw, err := c.conn.call(ctx, method, params)
		if err == nil || i >= attempts || !isContentModified(err) {
			return raw, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
}

func isContentModified(err error) bool {
	var e *rpcError
	return errors.As(err, &e) && e.Code == -32801
}

func (c *client) query(ctx context.Context, method, uri string, pos Position) (json.RawMessage, error) {
	return c.callRetry(ctx, method, map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
}

func (c *client) references(ctx context.Context, uri string, pos Position) (json.RawMessage, error) {
	return c.callRetry(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
		"context":      map[string]any{"includeDeclaration": true},
	})
}

// referencesSettled returns every reference, tolerating a freshly started
// server whose workspace index is still building. Right after the first
// didOpen, servers answer successfully but with only the queried file's own
// locations (pyright) or a transient ContentModified (gopls) — either way a
// cross-file query looks spuriously empty. The first call polls until the
// result spans other files or a short budget elapses; later calls return
// immediately (the index is warm by then).
func (c *client) referencesSettled(ctx context.Context, uri string, pos Position, queryFile string) ([]Location, error) {
	end := time.Now().Add(4 * time.Second)
	for {
		raw, err := c.references(ctx, uri, pos)
		if err != nil {
			if !isContentModified(err) || time.Now().After(end) {
				return nil, err
			}
		} else {
			locs := parseLocations(raw)
			if c.wasSettled() || !sameFileOnly(locs, queryFile) || time.Now().After(end) {
				c.markSettled()
				return locs, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *client) wasSettled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settled
}

func (c *client) markSettled() {
	c.mu.Lock()
	c.settled = true
	c.mu.Unlock()
}

func (c *client) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = c.conn.call(ctx, "shutdown", nil)
	_ = c.conn.notify("exit", nil)
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
