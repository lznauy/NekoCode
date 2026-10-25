package standard

import (
	"context"

	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
	"time"

	controlruntime "nekocode/runtime"
)

func (a *adapter) wireTaskLister(rt *controlruntime.Runtime) {
	a.bot.WireTaskLister(func(ctx context.Context, filter taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		select {
		case <-ctx.Done():
			return taskbridge.TaskPage{}, ctx.Err()
		default:
		}
		page, err := rt.Subagents(controlruntime.SubagentFilter{Query: filter.Query, Status: filter.Status, Offset: filter.Offset, Limit: filter.Limit})
		if err != nil {
			return taskbridge.TaskPage{}, err
		}
		out := taskbridge.TaskPage{SessionID: page.SessionID, Total: page.Total, NextOffset: page.NextOffset, HasMore: page.HasMore, Items: make([]taskbridge.TaskSummary, 0, len(page.Items))}
		for _, item := range page.Items {
			summary := taskbridge.TaskSummary{Recorded: item.Recorded, ID: item.ID, Title: item.Title, Profile: item.Profile, Status: item.Status, Prompt: item.Prompt, Result: item.Result, RunCount: item.RunCount}
			if attempt := item.LatestAttempt; attempt != nil {
				summary.LatestAttempt = &taskbridge.TaskAttemptSummary{ID: attempt.ID, Seq: attempt.Seq, Status: attempt.Status, Prompt: attempt.Prompt, Result: attempt.Result, TranscriptStart: attempt.TranscriptStart, TranscriptEnd: attempt.TranscriptEnd, TotalTokens: attempt.TotalTokens, CacheHitTokens: attempt.CacheHitTokens, CacheMissTokens: attempt.CacheMissTokens, ToolUseCount: attempt.ToolUseCount, ContextTokens: attempt.ContextTokens, ContextWindow: attempt.ContextWindow}
			}
			out.Items = append(out.Items, summary)
		}
		return out, nil
	})
}

func (a *adapter) Subagents(parentID string) ([]controlruntime.SubagentSummary, error) {
	infos, err := a.bot.Subagents(parentID)
	if err != nil {
		return nil, err
	}
	out := make([]controlruntime.SubagentSummary, 0, len(infos))
	for _, info := range infos {
		updated := time.Unix(info.UpdatedAt, 0)
		if info.UpdatedAtNano > 0 {
			updated = time.Unix(0, info.UpdatedAtNano)
		}
		var latest *controlruntime.SubagentAttemptSummary
		if info.LatestAttempt != nil {
			converted := convertSubagentAttempt(*info.LatestAttempt)
			latest = &converted
		}
		out = append(out, controlruntime.SubagentSummary{Recorded: info.Status != "unavailable", ID: info.ID, SessionID: info.ParentSessionID, Title: info.Title, Profile: info.Profile, Skills: info.Skills, Prompt: info.Prompt, Status: info.Status, RunCount: info.RunCount, Result: info.Result, Error: info.Error, TranscriptSeq: info.TranscriptSeq, CreatedAt: time.Unix(info.CreatedAt, 0), UpdatedAt: updated, LatestAttempt: latest})
	}
	return out, nil
}

func convertSubagentAttempt(attempt session.SubagentAttempt) controlruntime.SubagentAttemptSummary {
	ended := time.Time{}
	if attempt.EndedAt > 0 {
		ended = time.Unix(attempt.EndedAt, 0)
	}
	return controlruntime.SubagentAttemptSummary{
		ID: attempt.ID, Seq: attempt.Seq, Prompt: attempt.Prompt, Status: attempt.Status, Result: attempt.Result,
		StartedAt: time.Unix(attempt.StartedAt, 0), EndedAt: ended, TranscriptStart: attempt.TranscriptStart, TranscriptEnd: attempt.TranscriptEnd,
		TotalTokens: attempt.TotalTokens, CacheHitTokens: attempt.CacheHitTokens, CacheMissTokens: attempt.CacheMissTokens,
		ToolUseCount: attempt.ToolUseCount, DurationMs: attempt.DurationMs, ContextTokens: attempt.ContextTokens, ContextWindow: attempt.ContextWindow,
	}
}

func (a *adapter) SubagentAttempts(parentID, id string, before, limit int) (controlruntime.SubagentAttemptPage, error) {
	page, err := a.bot.SubagentAttempts(parentID, id, before, limit)
	if err != nil {
		return controlruntime.SubagentAttemptPage{}, err
	}
	out := controlruntime.SubagentAttemptPage{SessionID: parentID, SubagentID: id, Before: page.Before, End: page.End, Total: page.Total, HasMore: page.HasMore, Attempts: make([]controlruntime.SubagentAttemptSummary, 0, len(page.Attempts))}
	for _, attempt := range page.Attempts {
		out.Attempts = append(out.Attempts, convertSubagentAttempt(attempt))
	}
	return out, nil
}

func (a *adapter) SubagentHistory(parentID, id string, before, limit int) (controlruntime.SubagentHistoryPage, error) {
	page, err := a.bot.SubagentHistory(parentID, id, before, limit)
	if err != nil {
		return controlruntime.SubagentHistoryPage{}, err
	}
	out := controlruntime.SubagentHistoryPage{SessionID: parentID, SubagentID: id, Before: page.Before, End: page.End, Total: page.Total, HasMore: page.HasMore, Records: make([]controlruntime.SubagentRecord, 0, len(page.Messages))}
	contextHeader := len(page.Messages) > page.End-page.Before+1
	for i, msg := range page.Messages {
		record := controlruntime.SubagentRecord{Role: msg.Role, RuntimeEvent: msg.Source == types.MessageSourceRuntimeEvent, Content: msg.Content, Reasoning: msg.ReasoningContent, ToolCallID: msg.ToolCallID, IsError: msg.IsError, OutputPath: msg.ToolOutput.Path, OutputIncomplete: msg.ToolOutput.Incomplete}
		if contextHeader && i == 0 {
			record.ContextOnly = true
		} else {
			record.Sequence = page.Before + i
			if contextHeader {
				record.Sequence--
			}
		}
		for _, call := range msg.ToolCalls {
			record.ToolCalls = append(record.ToolCalls, controlruntime.SubagentToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
		}
		out.Records = append(out.Records, record)
	}
	return out, nil
}

func (a *adapter) SubagentToolOutput(parentID, id string, sequence int, offset int64) (controlruntime.SubagentOutputPage, error) {
	page, err := a.bot.SubagentToolOutput(parentID, id, sequence, offset)
	if err != nil {
		return controlruntime.SubagentOutputPage{}, err
	}
	return controlruntime.SubagentOutputPage{Content: page.Content, Offset: page.Offset, NextOffset: page.NextOffset, Total: page.Total, HasMore: page.HasMore, Incomplete: page.Incomplete}, nil
}
