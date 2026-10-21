// Command web-assistant assembles an application on the NekoCode agent and
// runtime foundations without the standard bot product. Its only tools are
// web_search and web_fetch; it has no plugin, MCP, skill, or coding tools.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	agentcore "nekocode/bot/agent"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/web"
	"nekocode/bot/provider"
	controlruntime "nekocode/runtime"
	"nekocode/runtime/agentrunner"
)

const assistantPrompt = `You are a concise research and chat assistant.
Use web_search when current information is needed and web_fetch to inspect a source.
Cite source URLs for factual claims based on the web.`

func newAssistant() *agentrunner.Runner {
	model := getenv("NEKOCODE_MODEL", "gpt-4o-mini")
	llm := provider.New(provider.Config{
		APIKey: os.Getenv("NEKOCODE_API_KEY"), BaseURL: os.Getenv("NEKOCODE_BASE_URL"),
		Model: model, Protocol: getenv("NEKOCODE_PROTOCOL", "openai"),
	})
	registry := tools.New(web.NewWebSearchTool(), web.NewWebFetchTool())
	agent := agentcore.New(context.Background(), agentcore.Config{
		Context: ctxmgr.New(ctxmgr.Config{
			SystemPrompt: assistantPrompt, ContextWindow: 128_000,
		}),
		Model: llm, Tools: registry,
	})
	return agentrunner.New(agent)
}

func main() {
	if os.Getenv("NEKOCODE_API_KEY") == "" {
		fmt.Println("set NEKOCODE_API_KEY to run this example")
		return
	}
	input := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if input == "" {
		input = "What changed in Go recently?"
	}

	runner := newAssistant()
	rt := controlruntime.New(runner, runner.ServiceGroups())
	defer func() {
		if err := rt.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close runtime: %v\n", err)
		}
	}()
	events, err := rt.Events(context.Background(), controlruntime.EventFilter{})
	if err != nil {
		panic(err)
	}
	runID, err := rt.StartRun(context.Background(), controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "example"}, Text: input,
	})
	if err != nil {
		panic(err)
	}
	for event := range events {
		if event.RunID != runID {
			continue
		}
		switch event.Type {
		case controlruntime.EventAssistantDelta:
			fmt.Print(event.Payload.(controlruntime.DeltaPayload).Delta)
		case controlruntime.EventRunDone:
			fmt.Println()
			return
		case controlruntime.EventRunFailed:
			fmt.Printf("\nerror: %s\n", event.Payload.(controlruntime.RunResult).Error)
			return
		}
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
