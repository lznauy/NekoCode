package sessionview

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nekocode/bot/llm/types"
	"nekocode/common"
)

// reImagePath matches image file paths in image_gen output.
// Supports both formats:
//
//	"  => /path/nekocode_img_xxx.jpg" (URL download with arrow)
//	"  /path/nekocode_img_xxx.jpg"    (base64, just the path)
//
// Recognizes files named nekocode_img_* with common image extensions.
var reImagePath = regexp.MustCompile(`(?:=>\s+)?(\S*(?:nekocode_img|/)\S*\.(?:png|jpg|jpeg|gif|webp))\b`)

func DisplayMessages(messages []types.Message, compactBoundary int) []common.DisplayMessage {
	if compactBoundary > 0 && compactBoundary < len(messages) {
		messages = messages[compactBoundary:]
	}

	toolNames, toolArgs := toolMetaByID(messages)
	var out []common.DisplayMessage
	i := 0
	for i < len(messages) {
		m := messages[i]
		switch m.Role {
		case "user":
			if !isInternalMessage(m) {
				out = append(out, common.DisplayMessage{Role: "user", Content: m.Content})
			}
			i++
		case "assistant":
			msg, next := displayAssistantMessage(messages, i, toolNames, toolArgs)
			if msg.Content != "" || len(msg.Blocks) > 0 || len(msg.Images) > 0 {
				out = append(out, msg)
			}
			i = next
		case "system":
			if !isInternalMessage(m) {
				out = append(out, common.DisplayMessage{Role: "system", Content: m.Content})
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

func displayAssistantMessage(msgs []types.Message, idx int, toolNames, toolArgs map[string]string) (common.DisplayMessage, int) {
	m := msgs[idx]
	var blocks []common.DisplayBlock
	var images []common.ImageRef
	next := idx + 1

	if len(m.ToolCalls) > 0 {
		for next < len(msgs) && msgs[next].Role == "tool" {
			name := toolNames[msgs[next].ToolCallID]
			if isPersistentTool(name) {
				blocks = append(blocks, common.DisplayBlock{
					ToolName: name,
					Args:     toolArgs[msgs[next].ToolCallID],
					Content:  msgs[next].Content,
					IsError:  msgs[next].IsError,
				})
			}
			if isImageTool(name) {
				refs := extractImageRefs(msgs[next].Content)
				images = append(images, refs...)
			}
			next++
		}
	}

	content := m.Content
	if len(m.ToolCalls) > 0 {
		content = ""
	}
	return common.DisplayMessage{
		Role:    "assistant",
		Content: content,
		Blocks:  blocks,
		Images:  images,
	}, next
}

func isPersistentTool(name string) bool {
	return name == "edit" || name == "write" || name == "bash"
}

// image tool name set — matches the tool Name() registered by ImageGenTool.
func isImageTool(name string) bool {
	return name == "image_gen"
}

// extractImageRefs parses image tool output text and returns ImageRef for each
// local path found, verifying file existence and reading dimensions.
// Skips URLs (Stat fails) — only actual local files are included.
func extractImageRefs(output string) []common.ImageRef {
	matches := reImagePath.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return nil
	}
	var refs []common.ImageRef
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
		ref := common.ImageRef{Path: abs}
		if dims := readImageDims(abs); dims != nil {
			ref.Width, ref.Height = dims[0], dims[1]
		}
		refs = append(refs, ref)
	}
	return refs
}

func isInternalMessage(msg types.Message) bool {
	return msg.Source == "hint" ||
		strings.Contains(msg.Content, "<hints>") ||
		strings.Contains(msg.Content, "<skill") ||
		strings.Contains(msg.Content, "<project-context>") ||
		strings.Contains(msg.Content, "<project>") ||
		strings.Contains(msg.Content, "Current working directory") ||
		strings.Contains(msg.Content, "<system-reminder>") ||
		strings.HasPrefix(msg.Content, "[Hook:")
}
