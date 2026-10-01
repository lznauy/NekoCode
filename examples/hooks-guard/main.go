// Command hooks-guard shows how to inject governance into an agent through
// agent.Config.Policy: a custom PreToolUse hook that blocks reads of .env
// files and dangerous shell commands. The agent sees the block reason and
// must answer without touching the protected files.
//
// Required environment:
//
//	NEKOCODE_API_KEY   API key for the provider (required)
//	NEKOCODE_BASE_URL  provider base URL (optional)
//	NEKOCODE_MODEL     model name (optional, default gpt-4o-mini)
//	NEKOCODE_PROTOCOL  wire protocol (optional, default openai)
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	agentcore "nekocode/bot/agent"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/builtin/catalog"
	"nekocode/bot/policy"
	"nekocode/bot/provider"
	"nekocode/protocol"
)

// guardHook blocks tool calls that touch .env files or run destructive
// shell commands. Hooks see a read-only snapshot of the call and return a
// verdict; BlockTool denies the call and reports the reason back to the model.
func guardHook() policy.Hook {
	return policy.Hook{
		Name: "guard", Point: policy.PreToolUse,
		On: func(s policy.State) *policy.Result {
			tool := s.Facts().Tool
			args := tool.Args
			if path, _ := args["path"].(string); strings.Contains(path, ".env") {
				return &policy.Result{BlockTool: &policy.BlockTool{
					Tool:   tool.Name,
					Reason: ".env files may contain secrets and must not be read or modified",
				}}
			}
			if cmd, _ := args["command"].(string); strings.Contains(cmd, "rm -rf /") {
				return &policy.Result{BlockTool: &policy.BlockTool{
					Tool:   tool.Name,
					Reason: "destructive command blocked by policy",
				}}
			}
			return nil
		},
	}
}

func main() {
	apiKey := os.Getenv("NEKOCODE_API_KEY")
	if apiKey == "" {
		fmt.Println("set NEKOCODE_API_KEY to run this example")
		return
	}
	model := getenv("NEKOCODE_MODEL", "gpt-4o-mini")

	llm := provider.New(provider.Config{
		APIKey: apiKey, BaseURL: os.Getenv("NEKOCODE_BASE_URL"),
		Model: model, Protocol: getenv("NEKOCODE_PROTOCOL", "openai"),
	})

	toolbox := catalog.NewToolboxWithConfig(catalog.ToolboxConfig{})
	defer func() {
		if err := toolbox.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "close toolbox:", err)
		}
	}()

	gov := policy.New()
	gov.Register(guardHook())

	agent := agentcore.New(context.Background(), agentcore.Config{
		Context: ctxmgr.New(ctxmgr.Config{
			SystemPrompt: "You are a helpful assistant.", ContextWindow: 128000,
		}),
		Model:  llm,
		Tools:  toolbox.Registry,
		Policy: gov,
	})

	result := agent.Run("读取当前目录下的 .env 文件，告诉我里面配置了哪些环境变量", func(ev protocol.StepEvent) {
		if ev.Action == protocol.StepActionToolBlocked {
			fmt.Printf("[blocked] %s: %s\n", ev.ToolName, ev.Output)
		}
	})
	fmt.Println("---")
	fmt.Println(result.FinalOutput)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
