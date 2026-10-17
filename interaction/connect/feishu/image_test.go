package feishu

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func strPtr(s string) *string { return &s }

func TestMessageImage(t *testing.T) {
	ev := &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Message: &larkim.EventMessage{
			MessageId: strPtr("om_1"),
			Content:   strPtr(`{"image_key":"img_v2_abc"}`),
		},
	}}
	messageID, key, ok := messageImage(ev)
	if !ok || messageID != "om_1" || key != "img_v2_abc" {
		t.Fatalf("messageImage = %q %q %v", messageID, key, ok)
	}

	// Missing image_key / bad JSON → not an image delivery.
	ev.Event.Message.Content = strPtr(`{"image_key":""}`)
	if _, _, ok := messageImage(ev); ok {
		t.Fatal("empty image_key should not parse")
	}
	ev.Event.Message.Content = strPtr(`not json`)
	if _, _, ok := messageImage(ev); ok {
		t.Fatal("invalid JSON should not parse")
	}
	if _, _, ok := messageImage(nil); ok {
		t.Fatal("nil event should not parse")
	}
}
