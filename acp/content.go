package acp

import (
	"encoding/base64"
	"fmt"
	"strings"

	controlruntime "nekocode/runtime"
	"nekocode/util/attachment"
)

// promptContent renders the client's prompt blocks into runtime input.
// Image blocks (base64 data, per the ACP spec) are stored in the session's
// attachment directory — which is why sessionID must be the active session —
// and referenced from the text via the [Image #N] marker convention shared
// with every other NekoCode entry point.
func promptContent(blocks []contentBlock, sessionID string) (string, []controlruntime.ImageAttachment, *wireError) {
	parts := make([]string, 0, len(blocks))
	var images []controlruntime.ImageAttachment
	imageCount := 0
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "resource_link":
			if block.Name == "" || block.URI == "" {
				return "", nil, rpcError(-32602, "resource_link requires name and uri")
			}
			link := fmt.Sprintf("[%s](%s)", block.Name, block.URI)
			if block.Description != "" {
				link += " — " + block.Description
			}
			parts = append(parts, link)
		case "image":
			if strings.TrimSpace(block.Data) == "" {
				return "", nil, rpcError(-32602, "image block requires base64 data")
			}
			data, err := base64.StdEncoding.DecodeString(block.Data)
			if err != nil {
				return "", nil, rpcError(-32602, "image block data is not valid base64: %v", err)
			}
			stored, err := attachment.SaveImage(sessionID, data)
			if err != nil {
				return "", nil, rpcError(-32000, "store image attachment: %v", err)
			}
			imageCount++
			marker := fmt.Sprintf("[Image #%d]", imageCount)
			parts = append(parts, marker)
			images = append(images, controlruntime.ImageAttachment{Label: marker, Path: stored})
		default:
			return "", nil, rpcError(-32602, "unsupported prompt content type %q", block.Type)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if text == "" {
		return "", nil, rpcError(-32602, "prompt is empty")
	}
	return text, images, nil
}
