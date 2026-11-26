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

	"nekocode/bot/extension/tool/runtime/core"
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

// DisplayMessagesWindow preserves raw positions even when the first tool
// batch has a synthetic assistant header prepended by the history reader.
func DisplayMessagesWindow(messages []types.Message, end int) []controlruntime.DisplayMessage {
	base := end - len(messages)
	out, _ := displayMessagePageAt(messages, 0, -1, &base)
	return out
}

func displayMessagePage(messages []types.Message, offset, limit int) ([]controlruntime.DisplayMessage, int) {
	return displayMessagePageAt(messages, offset, limit, nil)
}

func displayMessagePageAt(messages []types.Message, offset, limit int, base *int) ([]controlruntime.DisplayMessage, int) {
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
			msg, i, visible = displayAssistantTurn(messages, i, materialize)
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
				if base != nil {
					msg.Sequence = *base + i
				}
				out = append(out, msg)
			}
			total++
		}
	}
	return out, total
}

func displayAssistantTurn(msgs []types.Message, idx int, materialize bool) (controlruntime.DisplayMessage, int, bool) {
	visible := false
	var contentParts []string
	var blocks []controlruntime.DisplayBlock
	var images []controlruntime.ImageRef
	type taskIdentity struct {
		childID     string
		resultIndex int // distinct attempts that never acquired a child ID
	}
	tasks := make(map[taskIdentity]types.Message)

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

		// Call IDs belong to this assistant batch. A later model turn can reuse them.
		calls := make(map[string]types.FunctionCall, len(m.ToolCalls))
		for _, call := range m.ToolCalls {
			if call.ID != "" {
				calls[call.ID] = call.Function
			}
		}
		for next < len(msgs) && msgs[next].Role == "tool" {
			call := calls[msgs[next].ToolCallID]
			name := call.Name
			if name == "task" {
				visible = true
				if materialize {
					result := msgs[next]
					var args struct {
						ResumeID string `json:"resume_id"`
					}
					_ = json.Unmarshal([]byte(call.Arguments), &args)
					identity := taskIdentity{childID: args.ResumeID}
					text := strings.TrimPrefix(result.Content, "[Subagent returned a partial result]\n")
					if strings.HasPrefix(text, "[Subagent ID: ") {
						if id, _, ok := strings.Cut(strings.TrimPrefix(text, "[Subagent ID: "), "]"); ok {
							identity.childID = id
						}
					}
					if identity.childID == "" {
						identity.resultIndex = next
					}
					tasks[identity] = result
					partial := strings.HasPrefix(result.Content, "[Subagent returned a partial result]")
					if partial {
						blocks = append(blocks, controlruntime.DisplayBlock{ToolName: "子任务未完成", Content: textutil.NormalizeTerminalOutput(result.Content), IsError: true})
					} else if result.IsError {
						blocks = append(blocks, controlruntime.DisplayBlock{ToolName: "子任务失败", Content: textutil.NormalizeTerminalOutput(result.Content), IsError: true})
					}
				}
			}
			if materialize {
				blocks, images = appendDisplayToolResult(blocks, images, name, call.Arguments, msgs[next])
			} else if controlruntime.RetainsToolOutput(name) {
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
	if controlruntime.RetainsToolOutput(name) {
		text := msg.Content
		if name == "task_batch" && msg.ToolOutput.Path != "" {
			header := core.ArtifactHeader(msg.ToolOutput)
			if !strings.HasPrefix(text, header) {
				text = header + text
			}
		}
		blocks = append(blocks, controlruntime.DisplayBlock{
			ToolName: name,
			Args:     args,
			Content:  textutil.NormalizeTerminalOutput(text),
			IsError:  msg.IsError,
		})
	}
	if isImageTool(name) {
		images = append(images, extractImageRefs(msg.Content)...)
	}
	return blocks, images
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
	if msg.Role == "user" && !msg.IsConversationUser() {
		return true
	}
	switch msg.Source {
	case types.MessageSourceHint, types.MessageSourceRuntimeEvent, types.MessageSourceRuntimeContext,
		types.MessageSourceContextData, types.MessageSourceSkillWorkflow, types.MessageSourceVolatileTail:
		return true
	case "", "system":
		// Older transcripts did not tag injected user/system records. Only
		// recognize the entire envelope; quoted snippets are conversation.
	default:
		return false
	}
	if msg.Role != "system" && msg.Role != "user" {
		return false
	}
	content := strings.TrimSpace(msg.Content)
	for _, tag := range []string{"hints", "skill", "skill_content", "project-context", "project", "system-reminder"} {
		if (strings.HasPrefix(content, "<"+tag+">") || strings.HasPrefix(content, "<"+tag+" ")) && strings.HasSuffix(content, "</"+tag+">") {
			return true
		}
	}
	return msg.Role == "system" && (strings.HasPrefix(content, "Current working directory: ") ||
		strings.HasPrefix(content, "[Hook: ") && strings.Contains(content, "] "))
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
