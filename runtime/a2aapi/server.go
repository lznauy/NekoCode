// Package a2aapi exposes a NekoCode runtime as an Agent2Agent (A2A) v1 server.
package a2aapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
)

const maxRequestBodyBytes = 8 << 20

// Options configures the public A2A surface.
type Options struct {
	Endpoint     string
	AgentVersion string
	Secured      bool
}

// Server contains the public Agent Card and authenticated protocol handler.
// Authentication itself is supplied by the host so the existing daemon policy
// stays the single source of truth.
type Server struct {
	cardHandler http.Handler
	handler     http.Handler
}

func New(rt Runtime, options Options) (*Server, error) {
	if rt == nil {
		return nil, fmt.Errorf("a2a: nil runtime")
	}
	endpoint, err := normalizeEndpoint(options.Endpoint)
	if err != nil {
		return nil, err
	}
	card := newAgentCard(endpoint, options.AgentVersion, options.Secured)
	executor := newExecutor(rt)
	requestHandler := a2asrv.NewHandler(
		executor,
		a2asrv.WithCallInterceptors(localUserInterceptor{}),
		a2asrv.WithCapabilityChecks(&card.Capabilities),
		a2asrv.WithConcurrencyConfig(limiter.ConcurrencyConfig{MaxExecutions: 1}),
		a2asrv.WithTaskStore(newRetainedTaskStore(maxStoredTasks, a2asrv.NewTaskStoreAuthenticator())),
	)
	return &Server{
		cardHandler: a2asrv.NewStaticAgentCardHandler(card),
		handler:     http.MaxBytesHandler(a2asrv.NewRESTHandler(requestHandler), maxRequestBodyBytes),
	}, nil
}

func (s *Server) AgentCardHandler() http.Handler { return s.cardHandler }

func (s *Server) Handler() http.Handler { return s.handler }

func normalizeEndpoint(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("a2a: endpoint must be an absolute HTTP(S) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("a2a: endpoint scheme must be http or https")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("a2a: endpoint must not contain a query or fragment")
	}
	if u.User != nil {
		return "", fmt.Errorf("a2a: endpoint must not contain user information")
	}
	return raw, nil
}

// Authentication is enforced by the daemon before requests reach this
// handler. Assigning a stable local identity makes the SDK task store apply
// one consistent owner to send, get, list, cancel, and subscribe operations.
type localUserInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (localUserInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	callCtx.User = a2asrv.NewAuthenticatedUser("nekocode-daemon", nil)
	if req != nil {
		if send, ok := req.Payload.(*a2a.SendMessageRequest); ok && send != nil {
			if err := validateIncomingMessage(send.Message); err != nil {
				return ctx, nil, err
			}
		}
	}
	return ctx, nil, nil
}
