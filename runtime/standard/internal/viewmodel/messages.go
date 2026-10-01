package viewmodel

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nekocode/bot/provider/types"
	controlruntime "nekocode/runtime"
	textutil "nekocode/util/text"
)

var reImagePath = regexp.MustCompile(`(?:=>\s+)?(\S*(?:nekocode_img|/)\S*\.(?:png|jpg|jpeg|gif|webp))\b`)

func DisplayMessages(messages []types.Message) []controlruntime.DisplayMessage {
	toolNames, toolArgs := toolMetaByID(messages)
	var out []controlruntime.DisplayMessage
	i := 0
	for i < len(messages) {
		m := messages[i]
		switch m.Role {
		case "user":
			if !isInternalMessage(m) {
				out = append(out, controlruntime.DisplayMessage{Role: "user", Content: controlruntime.VisibleInputText(m.Content)})
			}
			i++
		case "assistant":
			msg, next := displayAssistantTurn(messages, i, toolNames, toolArgs)
			if msg.Content != "" || len(msg.Blocks) > 0 || len(msg.Images) > 0 {
				out = append(out, msg)
			}
			i = next
		case "system":
			if !isInternalMessage(m) {
				out = append(out, controlruntime.DisplayMessage{Role: "system", Content: m.Content})
			}
			i++
		default:
			i++
		}
	}
	return out
}

func toolMetaByID(msgs []types.Message) (names map[string]string, args map[string]string) {
	names = make(map[string]string, len(msgs))
	args = make(map[string]string, len(msgs))
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				names[tc.ID] = tc.Function.Name
				args[tc.ID] = tc.Function.Arguments
			}
		}
	}
	return names, args
}

func displayAssistantTurn(msgs []types.Message, idx int, toolNames, toolArgs map[string]string) (controlruntime.DisplayMessage, int) {
	var contentParts []string
	var blocks []controlruntime.DisplayBlock
	var images []controlruntime.ImageRef

	next := idx
	for next < len(msgs) && msgs[next].Role == "assistant" {
		m := msgs[next]
		next++

		if len(m.ToolCalls) == 0 {
			if content := strings.TrimSpace(m.Content); content != "" && !isInternalMessage(m) {
				contentParts = append(contentParts, content)
			}
			continue
		}

		for next < len(msgs) && msgs[next].Role == "tool" {
			name := toolNames[msgs[next].ToolCallID]
			blocks, images = appendDisplayToolResult(blocks, images, name, toolArgs[msgs[next].ToolCallID], msgs[next])
			next++
		}
	}

	return controlruntime.DisplayMessage{
		Role:    "assistant",
		Content: strings.Join(contentParts, "\n\n"),
		Blocks:  blocks,
		Images:  images,
	}, next
}

func appendDisplayToolResult(blocks []controlruntime.DisplayBlock, images []controlruntime.ImageRef, name, args string, msg types.Message) ([]controlruntime.DisplayBlock, []controlruntime.ImageRef) {
	if isPersistentTool(name) {
		blocks = append(blocks, controlruntime.DisplayBlock{
			ToolName: name,
			Args:     args,
			Content:  textutil.NormalizeTerminalOutput(msg.Content),
			IsError:  msg.IsError,
		})
	}
	if isImageTool(name) {
		images = append(images, extractImageRefs(msg.Content)...)
	}
	return blocks, images
}

func isPersistentTool(name string) bool {
	return name == "edit" || name == "diff" || name == "write" || name == "shell" || name == "bash" || name == "process"
}

func isImageTool(name string) bool {
	return name == "image_gen"
}

func extractImageRefs(output string) []controlruntime.ImageRef {
	matches := reImagePath.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return nil
	}
	var refs []controlruntime.ImageRef
	for _, m := range matches {
		path := m[1]
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		ref := controlruntime.ImageRef{Path: abs}
		if dims := readImageDims(abs); dims != nil {
			ref.Width, ref.Height = dims[0], dims[1]
		}
		refs = append(refs, ref)
	}
	return refs
}

func isInternalMessage(msg types.Message) bool {
	return msg.Source == types.MessageSourceHint || msg.Source == types.MessageSourceRuntimeContext ||
		msg.Source == types.MessageSourceRuntimeEvent ||
		strings.Contains(msg.Content, "<hints>") ||
		strings.Contains(msg.Content, "<skill") ||
		strings.Contains(msg.Content, "<project-context>") ||
		strings.Contains(msg.Content, "<project>") ||
		strings.Contains(msg.Content, "Current working directory") ||
		strings.Contains(msg.Content, "<system-reminder>") ||
		strings.HasPrefix(msg.Content, "[Hook:")
}

func readImageDims(path string) []int {
	ext := strings.ToLower(path[strings.LastIndexByte(path, '.'):])
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil
	}
	return []int{cfg.Width, cfg.Height}
}
