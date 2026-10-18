package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	controlruntime "nekocode/runtime"
)

// Backend is the transport-neutral runtime surface used by the ACP adapter.
type Backend interface {
	controlruntime.RunControl
	controlruntime.RunEvents
	controlruntime.HumanInteraction
	controlruntime.SessionManagement
	controlruntime.ModelReader
	WaitRun(context.Context, controlruntime.RunID) error
	ReplaceMCPServers(context.Context, string, []controlruntime.MCPServerSpec) error
	// Session config surface: model selection, reasoning effort and the
	// full-takeover permission mode.
	SwitchSessionModel(string) (controlruntime.ModelSelection, error)
	SetSessionReasoning(string) error
	SetFullAccess(bool) error
	ApplySessionSettings(controlruntime.SessionSettings) error
	PermissionMode() string
	// Metrics reports the current context usage snapshot for usage updates.
	Metrics() controlruntime.MetricsSnapshot
}

type server struct {
	backend Backend
	cwd     string
	conn    *connection

	mu          sync.Mutex
	sessionMu   sync.Mutex
	initialized bool
	// clientBooleanConfig records whether the client advertised support for
	// boolean session config options during initialize.
	clientBooleanConfig bool
	active              map[string]*activeTurn
	allowClientMCP      bool
	defaultConfig       sessionConfig
	defaultEfforts      map[string]string
	sessionConfigs      map[string]sessionConfig
	sessionMCPConfigs   map[string][]controlruntime.MCPServerSpec
	activeMCPSession    string
}

// ServerOptions controls trust-sensitive ACP capabilities.
type ServerOptions struct {
	// AllowClientMCP permits the ACP peer to launch session-supplied stdio MCP
	// processes. Keep false unless the peer and its workspace configuration are
	// trusted.
	AllowClientMCP bool
}

type activeTurn struct {
	runID     controlruntime.RunID
	cancel    context.CancelFunc
	cancelled bool
}

// Serve runs an ACP v1 server over the supplied newline-delimited stream.
// The caller owns the backend lifecycle.
func Serve(ctx context.Context, in io.Reader, out io.Writer, runtime Backend, cwd string) error {
	return ServeWithOptions(ctx, in, out, runtime, cwd, ServerOptions{})
}

// ServeWithOptions runs an ACP server with explicit trust-sensitive options.
func ServeWithOptions(ctx context.Context, in io.Reader, out io.Writer, runtime Backend, cwd string, options ServerOptions) error {
	if runtime == nil {
		return errors.New("acp: nil backend")
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("acp: resolve working directory: %w", err)
	}
	s := &server{
		backend: runtime, cwd: filepath.Clean(absCWD), active: make(map[string]*activeTurn),
		allowClientMCP: options.AllowClientMCP, sessionConfigs: make(map[string]sessionConfig),
		sessionMCPConfigs: make(map[string][]controlruntime.MCPServerSpec),
	}
	models, _ := runtime.ModelOptions()
	s.defaultEfforts = make(map[string]string, len(models))
	for _, model := range models {
		s.defaultEfforts[model.Name] = model.ReasoningEffort
	}
	s.defaultConfig = s.currentSessionConfig()
	s.conn = newConnection(out)
	serveErr := s.conn.serve(ctx, in, s.handle)
	cleanupErr := s.cleanup()
	return errors.Join(serveErr, cleanupErr)
}

func (s *server) handle(ctx context.Context, method string, params json.RawMessage) (any, *wireError) {
	if method == "initialize" {
		return s.initialize(params)
	}
	s.mu.Lock()
	initialized := s.initialized
	s.mu.Unlock()
	if !initialized {
		return nil, rpcError(-32002, "connection is not initialized")
	}

	switch method {
	case "session/new":
		return s.newSession(ctx, params)
	case "session/load":
		return s.loadSession(ctx, params)
	case "session/list":
		return s.listSessions(params)
	case "session/delete":
		return s.deleteSession(ctx, params)
	case "session/prompt":
		return s.prompt(ctx, params)
	case "session/set_config_option":
		return s.setConfigOption(params)
	case "session/cancel":
		return struct{}{}, s.cancel(ctx, params)
	default:
		return nil, rpcError(-32601, "method not found: %s", method)
	}
}

func backendError(action string, err error) *wireError {
	return rpcError(-32603, "%s: %v", action, err)
}

func (s *server) notifySession(sessionID string, update map[string]any) error {
	return s.conn.notify("session/update", map[string]any{"sessionId": sessionID, "update": update})
}

var _ Backend = (*controlruntime.Runtime)(nil)
