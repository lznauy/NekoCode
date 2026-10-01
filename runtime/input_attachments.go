package runtime

import (
	"encoding/json"
	"strings"
)

const imageAttachmentsStart = "\n\n<image_attachments>\n"
const imageAttachmentsEnd = "\n</image_attachments>"

const (
	imageAttachmentEnvelopeKind    = "nekocode.image_attachments"
	imageAttachmentEnvelopeVersion = 1
	imageAttachmentInstruction     = "Use the image_understand tool with the corresponding local path when image contents are needed."
)

const MaxImageAttachments = 8

type imageAttachmentEnvelope struct {
	Kind        string            `json:"kind"`
	Version     int               `json:"version"`
	Images      []ImageAttachment `json:"images"`
	Instruction string            `json:"instruction"`
}

// InputWithImageAttachments adds model-only attachment metadata while the
// visible input remains the user's placeholder text.
func InputWithImageAttachments(text string, images []ImageAttachment) string {
	valid := ValidImageAttachments(text, images)
	if len(valid) == 0 {
		return text
	}
	payload, err := json.Marshal(imageAttachmentEnvelope{
		Kind:        imageAttachmentEnvelopeKind,
		Version:     imageAttachmentEnvelopeVersion,
		Images:      valid,
		Instruction: imageAttachmentInstruction,
	})
	if err != nil {
		return text
	}
	return text + imageAttachmentsStart + string(payload) + imageAttachmentsEnd
}

// ValidImageAttachments returns only attachments that can be represented in
// the model input. It is also used by input-accepted events so UI ownership is
// transferred for exactly the files that reached the agent.
func ValidImageAttachments(text string, images []ImageAttachment) []ImageAttachment {
	valid := make([]ImageAttachment, 0, len(images))
	for _, image := range images {
		if len(valid) == MaxImageAttachments {
			break
		}
		if strings.TrimSpace(image.Label) == "" || strings.TrimSpace(image.Path) == "" || !strings.Contains(text, image.Label) {
			continue
		}
		valid = append(valid, image)
	}
	return valid
}

// VisibleInputText removes runtime-added attachment metadata from persisted
// user messages while preserving the placeholders the user saw.
func VisibleInputText(text string) string {
	index := strings.LastIndex(text, imageAttachmentsStart)
	if index < 0 || !strings.HasSuffix(text, imageAttachmentsEnd) {
		return text
	}
	payload := strings.TrimSuffix(text[index+len(imageAttachmentsStart):], imageAttachmentsEnd)
	var envelope imageAttachmentEnvelope
	if json.Unmarshal([]byte(payload), &envelope) != nil ||
		envelope.Kind != imageAttachmentEnvelopeKind ||
		envelope.Version != imageAttachmentEnvelopeVersion ||
		envelope.Instruction != imageAttachmentInstruction ||
		len(envelope.Images) == 0 || len(envelope.Images) > MaxImageAttachments {
		return text
	}
	visible := strings.TrimRight(text[:index], "\n")
	for _, image := range envelope.Images {
		if strings.TrimSpace(image.Label) == "" || strings.TrimSpace(image.Path) == "" || !strings.Contains(visible, image.Label) {
			return text
		}
	}
	return visible
}
