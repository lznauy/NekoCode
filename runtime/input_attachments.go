package runtime

import (
	"encoding/json"
	"os"
	"strings"

	"nekocode/util/attachment"
)

const imageAttachmentsStart = "\n\n<image_attachments>\n"
const imageAttachmentsEnd = "\n</image_attachments>"

const (
	imageAttachmentEnvelopeKind    = "nekocode.image_attachments"
	imageAttachmentEnvelopeVersion = 1
	// imageAttachmentInstruction is used when the model cannot see images
	// natively and must go through the image_understand tool.
	imageAttachmentInstruction = "Use the image_understand tool with the corresponding local path when image contents are needed."
	// imageAttachmentVisionInstruction is used when the active model accepts
	// native image input. It is worded conditionally rather than
	// prohibitively: sessions survive model switches, and a blanket ban
	// would strand a non-vision successor (images stripped, tool banned)
	// with no way to read the images at all. Vision models read the
	// condition as satisfied and answer directly; non-vision models fall
	// through to the tool.
	imageAttachmentVisionInstruction = "These images are delivered natively as image content parts when the active model supports vision. If they are not visible in this request, read them with the image_understand tool using the local paths above."
	// imageAttachmentVisionInstructionLegacy and ...LegacyDirect are earlier
	// wordings still found in persisted sessions; accepted for envelope
	// stripping so historical messages keep rendering without the raw
	// envelope.
	imageAttachmentVisionInstructionLegacy       = "These images are also delivered natively as image content parts. Use the image_understand tool with the corresponding local path only when a closer look at a specific image is needed."
	imageAttachmentVisionInstructionLegacyDirect = "These images are delivered natively as image content parts; view them directly. Do not call the image_understand tool unless the user explicitly asks for it."
)

const MaxImageAttachments = 8

type imageAttachmentEnvelope struct {
	Kind        string            `json:"kind"`
	Version     int               `json:"version"`
	Images      []ImageAttachment `json:"images"`
	Instruction string            `json:"instruction"`
}

// InputWithImageAttachments adds model-only attachment metadata while the
// visible input remains the user's placeholder text. The envelope is always
// injected — including on vision models — so switching to a non-vision model
// mid-session still leaves every image path reachable. When vision is
// enabled the images are additionally delivered as native content parts.
func InputWithImageAttachments(text string, images []ImageAttachment, vision bool) string {
	valid := ValidImageAttachments(text, images)
	if len(valid) == 0 {
		return text
	}
	instruction := imageAttachmentInstruction
	if vision {
		instruction = imageAttachmentVisionInstruction
	}
	payload, err := json.Marshal(imageAttachmentEnvelope{
		Kind:        imageAttachmentEnvelopeKind,
		Version:     imageAttachmentEnvelopeVersion,
		Images:      valid,
		Instruction: instruction,
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

// adoptImageAttachments copies inbound attachment files into the session's
// attachment storage (util/attachment). IM connectors download images to
// os-level temp directories, which workspace-gated tools (image_understand)
// refuse to read; session storage carries a per-session read-only workspace
// root. Attachments already inside storage (GUI/TUI paste path) and
// unreadable sources pass through untouched.
func (r *Runtime) adoptImageAttachments(input Input) Input {
	if len(input.Images) == 0 || r.services.Sessions.CurrentSessionID == nil {
		return input
	}
	sessionID := r.services.Sessions.CurrentSessionID()
	dir, err := attachment.ImageDir(sessionID)
	if sessionID == "" || err != nil {
		return input
	}
	adopted := append([]ImageAttachment(nil), input.Images...)
	for i, img := range adopted {
		if img.Path == "" || strings.HasPrefix(img.Path, dir+string(os.PathSeparator)) {
			continue
		}
		data, err := os.ReadFile(img.Path)
		if err != nil {
			continue
		}
		stored, err := attachment.SaveImage(sessionID, data)
		if err != nil {
			continue
		}
		adopted[i] = ImageAttachment{Label: img.Label, Path: stored}
		// The connector-side download copy in the system temp dir is
		// redundant once the image lives in session storage.
		if strings.HasPrefix(img.Path, os.TempDir()+string(os.PathSeparator)) {
			_ = os.Remove(img.Path)
		}
	}
	input.Images = adopted
	return input
}

// VisibleInputText removes runtime-added attachment metadata from persisted
// user messages while preserving the placeholders the user saw. All
// instruction variants ever emitted (tool proxy, conditional vision, and the
// two legacy vision wordings) are accepted so old sessions stay strippable.
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
		(envelope.Instruction != imageAttachmentInstruction &&
			envelope.Instruction != imageAttachmentVisionInstruction &&
			envelope.Instruction != imageAttachmentVisionInstructionLegacy &&
			envelope.Instruction != imageAttachmentVisionInstructionLegacyDirect) ||
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
