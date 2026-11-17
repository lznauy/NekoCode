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
	store := newRetainedTaskStore(maxStoredTasks, a2asrv.NewTaskStoreAuthenticator())
	executor.store = store
	requestHandler := a2asrv.NewHandler(
		executor,
		a2asrv.WithCallInterceptors(localUserInterceptor{executor: executor}),
		a2asrv.WithCapabilityChecks(&card.Capabilities),
		a2asrv.WithConcurrencyConfig(limiter.ConcurrencyConfig{MaxExecutions: 1}),
		a2asrv.WithTaskStore(store),
	)
	// Keep the SDK's authentication/interceptor boundary around subscriptions
	// while replacing its execution-only subscription source.
	intercepted := requestHandler.(*a2asrv.InterceptedHandler)
	intercepted.Handler = &taskSubscriptionHandler{RequestHandler: intercepted.Handler, store: store}
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
	executor *executor
}

type monitorHandoffKey struct{}

type monitorHandoff struct {
	taskID  a2a.TaskID
	binding *taskBinding
	owned   bool
	paused  bool
}

func (i localUserInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	callCtx.User = a2asrv.NewAuthenticatedUser("nekocode-daemon", nil)
	var taskID a2a.TaskID
	if req != nil {
		if send, ok := req.Payload.(*a2a.SendMessageRequest); ok && send != nil {
			if err := validateIncomingMessage(send.Message); err != nil {
				return ctx, nil, err
			}
			taskID = send.Message.TaskID
		}
		if cancel, ok := req.Payload.(*a2a.CancelTaskRequest); ok && cancel != nil {
			taskID = cancel.ID
		}
	}
	// Stop background updates before the SDK loads the task and appends the
	// incoming message; stopping only in Execute is too late for version checks.
	if i.executor != nil && taskID != "" {
		if binding := i.executor.lookup(taskID); binding != nil {
			binding.mu.Lock()
			binding.monitorPauses++
			binding.foreground++
			binding.mu.Unlock()
			i.executor.stopMonitor(binding)
			ctx = context.WithValue(ctx, monitorHandoffKey{}, &monitorHandoff{taskID: taskID, binding: binding, owned: true, paused: true})
		}
	}
	return ctx, nil, nil
}

func (i localUserInterceptor) After(ctx context.Context, _ *a2asrv.CallContext, response *a2asrv.Response) error {
	// A rejected request must relinquish only its own foreground reservation;
	// another request may already own the SDK execution for this task.
	if i.executor == nil {
		return nil
	}
	handoff, _ := ctx.Value(monitorHandoffKey{}).(*monitorHandoff)
	if handoff == nil {
		return nil
	}
	binding := handoff.binding
	binding.mu.Lock()
	if handoff.paused {
		binding.monitorPauses--
		handoff.paused = false
	}
	binding.mu.Unlock()
	if response.Err != nil {
		i.executor.releaseForeground(ctx, binding)
	}
	if stored, err := i.executor.store.Get(ctx, handoff.taskID); err == nil && !stored.Task.Status.State.Terminal() {
		i.executor.startMonitor(ctx, &a2asrv.ExecutorContext{TaskID: handoff.taskID, ContextID: stored.Task.ContextID})
	}
	return nil
}
