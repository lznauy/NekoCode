package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nekocode/bot/debug"
	"nekocode/bot/tools"
)

const fallbackNoAction = "Sorry, I couldn't determine what to do"

type ActionType int

const (
	ActionChat ActionType = iota
	ActionExecuteTool
)

func (a ActionType) String() string {
	switch a {
	case ActionChat:
		return "chat"
	case ActionExecuteTool:
		return "execute_tool"
	default:
		return "unknown"
	}
}

type Result struct {
	Thought         string
	Action          ActionType
	ActionInput     string
	ToolCalls       []tools.ToolCallItem
	TextContent     string
	Interrupted     bool
	GarbledToolCall bool
	IsError         bool
}

func CommandResult() *Result {
	return &Result{Thought: "User entered a command", Action: ActionChat}
}

func FromLLM(toolCalls []tools.ToolCallItem, textContent string, err error) *Result {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return &Result{Thought: "User interrupted", Action: ActionChat, Interrupted: true}
		}
		if textContent != "" && !IsGarbledToolCall(textContent) {
			return &Result{Thought: "Truncated reply", Action: ActionChat, ActionInput: textContent}
		}
		return &Result{Thought: "LLM call failed", Action: ActionChat, ActionInput: fmt.Sprintf("LLM call failed: %v", err), IsError: true}
	}

	if len(toolCalls) == 0 {
		if IsGarbledToolCall(textContent) {
			debug.Log("GarbledToolCall: XML leaked (len=%d)", len(textContent))
			return &Result{Thought: "Format correction", Action: ActionChat, GarbledToolCall: true}
		}
		if textContent == "" {
			textContent = fallbackNoAction
		}
		return &Result{Thought: "Direct reply", Action: ActionChat, ActionInput: textContent}
	}

	if len(toolCalls) == 1 {
		tc := toolCalls[0]
		return &Result{
			Thought:     "Call tool: " + tc.Name,
			Action:      ActionExecuteTool,
			ActionInput: tc.Name + ":" + tools.FormatArgs(tc.Args),
			ToolCalls:   toolCalls,
			TextContent: textContent,
		}
	}

	var names []string
	for _, tc := range toolCalls {
		names = append(names, tc.Name)
	}
	return &Result{
		Thought:     "Parallel tool calls: " + strings.Join(names, ", "),
		Action:      ActionExecuteTool,
		ToolCalls:   toolCalls,
		TextContent: textContent,
	}
}

func IsGarbledToolCall(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if strings.Contains(t, "<invoke") || strings.Contains(t, "</invoke") ||
		strings.Contains(t, "<parameter") || strings.Contains(t, "</parameter") ||
		strings.Contains(t, "<tool_call") || strings.Contains(t, "</tool_call") {
		return true
	}
	return strings.Contains(t, `"tool_calls"`) || strings.Contains(t, `"tool_use"`)
}
