package viewmodel

import (
	"encoding/json"
	"fmt"
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
	out, _ := displayMessagePage(messages, 0, -1)
	return out
}

// DisplayMessagesPage counts visible groups but only materializes the requested
// page. Off-page terminal output and image dimensions are never decoded.
func DisplayMessagesPage(messages []types.Message, offset, limit int) (controlruntime.SessionMessagePage, error) {
	if offset < 0 || limit < 1 || limit > 1000 {
		return controlruntime.SessionMessagePage{}, fmt.Errorf("offset must be within history; limit must be 1..1000")
	}
	out, total := displayMessagePage(messages, offset, limit)
	if offset > total {
		return controlruntime.SessionMessagePage{}, fmt.Errorf("offset must be within history; limit must be 1..1000")
	}
	if total > 0 && out == nil {
		out = []controlruntime.DisplayMessage{}
	}
	next := offset + len(out)
	return controlruntime.SessionMessagePage{Messages: out, NextOffset: next, HasMore: next < total, Total: total}, nil
}

func displayMessagePage(messages []types.Message, offset, limit int) ([]controlruntime.DisplayMessage, int) {
	toolNames, toolArgs := toolMetaByID(messages)
	var out []controlruntime.DisplayMessage
	total := 0
	for i := 0; i < len(messages); {
		m := messages[i]
		materialize := total >= offset && (limit < 0 || len(out) < limit)
		var msg controlruntime.DisplayMessage
		visible := false
		switch m.Role {
		case "user":
			visible = !isInternalMessage(m)
			if materialize && visible {
				msg = controlruntime.DisplayMessage{Role: "user", Content: controlruntime.VisibleInputText(m.Content)}
			}
			i++
		case "assistant":
			msg, i, visible = displayAssistantTurn(messages, i, toolNames, toolArgs, materialize)
		case "system":
			visible = !isInternalMessage(m)
			if materialize && visible {
				msg = controlruntime.DisplayMessage{Role: "system", Content: m.Content}
			}
			i++
		default:
			i++
		}
		if visible {
			if materialize {
				out = append(out, msg)
			}
			total++
		}
	}
	return out, total
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

func displayAssistantTurn(msgs []types.Message, idx int, toolNames, toolArgs map[string]string, materialize bool) (controlruntime.DisplayMessage, int, bool) {
	visible := false
	var contentParts []string
	var blocks []controlruntime.DisplayBlock
	var images []controlruntime.ImageRef
	tasks := make(map[string]types.Message)

	next := idx
	for next < len(msgs) && msgs[next].Role == "assistant" {
		m := msgs[next]
		next++

		if len(m.ToolCalls) == 0 {
			if content := strings.TrimSpace(m.Content); content != "" && !isInternalMessage(m) {
				visible = true
				if materialize {
					contentParts = append(contentParts, content)
				}
			}
			continue
		}

		for next < len(msgs) && msgs[next].Role == "tool" {
			name := toolNames[msgs[next].ToolCallID]
			if name == "task" {
				visible = true
				if materialize {
					result := msgs[next]
					identity := result.ToolCallID
					var args struct {
						ResumeID string `json:"resume_id"`
					}
					_ = json.Unmarshal([]byte(toolArgs[identity]), &args)
					if args.ResumeID != "" {
						identity = args.ResumeID
					}
					text := strings.TrimPrefix(result.Content, "[Subagent returned a partial result]\n")
					if strings.HasPrefix(text, "[Subagent ID: ") {
						if id, _, ok := strings.Cut(strings.TrimPrefix(text, "[Subagent ID: "), "]"); ok {
							identity = id
						}
					}
					tasks[identity] = result
					partial := strings.HasPrefix(result.Content, "[Subagent returned a partial result]")
					if result.IsError && !partial {
						blocks = append(blocks, controlruntime.DisplayBlock{ToolName: "子任务失败", Content: textutil.NormalizeTerminalOutput(result.Content), IsError: true})
					}
				}
			}
			if materialize {
				blocks, images = appendDisplayToolResult(blocks, images, name, toolArgs[msgs[next].ToolCallID], msgs[next])
			} else if isPersistentTool(name) {
				visible = true
			} else if isImageTool(name) && len(imageRefs(msgs[next].Content, false)) > 0 {
				visible = true
			}
			next++
		}
	}
	if len(tasks) > 0 {
		var completed, partial, failed int
		for _, result := range tasks {
			if strings.HasPrefix(result.Content, "[Subagent returned a partial result]") {
				partial++
			} else if result.IsError {
				failed++
			} else {
				completed++
			}
		}
		var parts []string
		for _, count := range []struct {
			name string
			n    int
		}{{"完成", completed}, {"失败", failed}, {"未完成", partial}} {
			if count.n > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", count.name, count.n))
			}
		}
		blocks = append(blocks, controlruntime.DisplayBlock{ToolName: "子任务", Content: strings.Join(parts, " · ")})
	}

	return controlruntime.DisplayMessage{
		Role:    "assistant",
		Content: strings.Join(contentParts, "\n\n"),
		Blocks:  blocks,
		Images:  images,
	}, next, visible || len(blocks) > 0 || len(images) > 0
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
	return imageRefs(output, true)
}

func imageRefs(output string, dimensions bool) []controlruntime.ImageRef {
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
		if dimensions {
			if dims := readImageDims(abs); dims != nil {
				ref.Width, ref.Height = dims[0], dims[1]
			}
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
