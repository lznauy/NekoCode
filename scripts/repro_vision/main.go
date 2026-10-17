// Command reproduce: replays the real NekoCode model call path (full tool
// catalog + provider + image message) against the configured GLM endpoint to
// determine whether native image parts survive the real request shape.
//
// Env knobs:
//
//	WITH_IMAGE=0   omit native image parts (token baseline)
//	SYSTEM_FROM=…  path to a session.json; its system_prompt is prepended
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/builtin/catalog"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
)

const imagePath = "/home/lznauy/.nekocode/tmp/images/20261015T142035-056885799/paste-e0b5518679e77cc2.jpg"

const userText = "[Image #1]\n\n<image_attachments>\n" +
	`{"kind":"nekocode.image_attachments","version":1,"images":[{"label":"[Image #1]","path":"` + imagePath + `"}],"instruction":"These images are delivered natively as image content parts when the active model supports vision. If they are not visible in this request, read them with the image_understand tool using the local paths above."}` +
	"\n</image_attachments>"

func main() {
	raw, err := os.ReadFile(os.Getenv("HOME") + "/.nekocode/config.json")
	if err != nil {
		panic(err)
	}
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		panic(err)
	}
	model := cfg.ActiveModelConfig()
	fmt.Println("active model:", model.Name, model.Model, "vision-effective:", model.EffectiveVision())

	// Full tool catalog, exactly as the agent builds it.
	box := catalog.NewToolboxWithConfig(catalog.ToolboxConfig{})
	toolDefs := toolcore.ToToolDefs(box.Registry.Descriptors())
	fmt.Println("tools:", len(toolDefs))

	messages := []types.Message{{Role: "user", Content: userText}}
	if os.Getenv("WITH_IMAGE") != "0" {
		messages[0].Images = []types.MessageImage{{Path: imagePath}}
	}
	if sp := os.Getenv("SYSTEM_FROM"); sp != "" {
		rawSession, err := os.ReadFile(sp)
		if err == nil {
			var session struct {
				SystemPrompt string `json:"system_prompt"`
			}
			if json.Unmarshal(rawSession, &session) == nil && session.SystemPrompt != "" {
				messages = append([]types.Message{{Role: "system", Content: session.SystemPrompt}}, messages...)
				fmt.Println("system prompt:", len(session.SystemPrompt), "chars")
			}
		}
	}

	client := provider.New(provider.Config{
		APIKey: model.APIKey, BaseURL: model.BaseURL, Model: model.Model, Protocol: model.Protocol,
		Vision: model.EffectiveVision(),
	})

	tokenCh, errCh := client.ChatStream(context.Background(), messages, toolDefs)
	start := time.Now()
	var content, reasoning string
	var finish string
	var usage types.StreamUsage
loop:
	for {
		select {
		case token, ok := <-tokenCh:
			if !ok {
				break loop
			}
			content += token.Content
			reasoning += token.ReasoningContent
			if token.FinishReason != "" {
				finish = token.FinishReason
			}
			if token.Usage != nil {
				usage = *token.Usage
			}
		case err := <-errCh:
			if err != nil {
				fmt.Println("stream error:", err)
				return
			}
			break loop
		}
	}
	fmt.Println("dur:", time.Since(start))
	fmt.Println("finish:", finish)
	fmt.Printf("usage: in=%d out=%d\n", usage.PromptTokens, usage.CompletionTokens)
	fmt.Println("reasoning:", trim(reasoning))
	fmt.Println("content:", trim(content))
}

func trim(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	if s == "" {
		return "(EMPTY)"
	}
	return s
}
