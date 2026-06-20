// Package contextmgr provides a layered context management system for LLM conversations.
//
// Design rationale: the sub-packages (compact, context, memory, token) are
// organized by domain responsibility rather than as a monolithic module.
// Each sub-package has its own test suite and can be evolved independently.
// This is intentional — aggressive merging would destroy test isolation
// without measurable benefit.
package contextmgr

import (
	"context"
	"fmt"
	"sync"

	"nekocode/bot/contextmgr/compact"
	ctxctx "nekocode/bot/contextmgr/context"
	"nekocode/bot/contextmgr/memory"
	"nekocode/bot/contextmgr/token"
	"nekocode/llm/types"
)

type Manager struct {
	mu            sync.RWMutex
	ctx           ctxctx.Content
	ContextWindow int
	Tracker       *token.Tracker
	CompactCount  int
	TrimCount     int
	mem           *memory.File
	CM            *compact.Compactor
	MergeClient   types.LLM // for independent merge archive sessions
}

type Config struct {
	SystemPrompt string
	Memory       *memory.File
	Summarizer   compact.Summarizer
	MergeClient  types.LLM
}

// NewSub creates a lightweight Manager for subagents.
// A Compactor is only created when mergeClient is non-nil (for archive merging).
func NewSub(systemPrompt string, contextWindow int, mergeClient types.LLM) *Manager {
	ctx := ctxctx.New(systemPrompt)
	m := &Manager{
		ctx:           ctx,
		Tracker:       &token.Tracker{},
		ContextWindow: contextWindow,
	}
	if mergeClient != nil {
		mergeCtx := context.Background()
		m.CM = &compact.Compactor{
			Ctx:           &m.ctx,
			ContextWindow: &m.ContextWindow,
			Tracker:       m.Tracker,
			CompactCount:  &m.CompactCount,
			TrimCount:     &m.TrimCount,
			Summarizer:    MakeSummarizer(mergeCtx, mergeClient),
			CancelCtx:     mergeCtx,
			Cfg:           compact.DefaultConfig,
		}
	}
	return m
}

// MakeSummarizer creates a Summarizer func from an LLM client.
// The provided context is used for LLM calls, enabling cancellation.
func MakeSummarizer(ctx context.Context, client types.LLM) compact.Summarizer {
	return func(msgs []types.Message, prevSummary string) (string, error) {
		prompt := compact.BuildPrompt(msgs, prevSummary)
		resp, err := client.Chat(ctx, []types.Message{{Role: "user", Content: prompt}}, nil)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
			return resp.Choices[0].Message.Content, nil
		}
		return "", fmt.Errorf("no response from summarizer")
	}
}

func New(cfg Config) *Manager {
	ctx := ctxctx.New(cfg.SystemPrompt)
	if cfg.Memory != nil {
		ctx.Memory = cfg.Memory.Build()
	}
	m := &Manager{
		ctx:         ctx,
		Tracker:     &token.Tracker{},
		mem:         cfg.Memory,
		MergeClient: cfg.MergeClient,
	}
	m.CM = &compact.Compactor{
		Ctx:           &m.ctx,
		ContextWindow: &m.ContextWindow,
		Tracker:       m.Tracker,
		CompactCount:  &m.CompactCount,
		TrimCount:     &m.TrimCount,
		Summarizer:    cfg.Summarizer,
		CancelCtx:     context.Background(),
		Cfg:           compact.DefaultConfig,
	}
	return m
}
