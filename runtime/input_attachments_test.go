package runtime

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGUIImageAttachmentLimitMatchesRuntime(t *testing.T) {
	source, err := os.ReadFile("../interaction/gui/web/src/hooks/useChat.ts")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`const MAX_IMAGE_ATTACHMENTS = (\d+)`).FindSubmatch(source)
	if len(match) != 2 {
		t.Fatal("GUI must declare MAX_IMAGE_ATTACHMENTS for protocol consistency")
	}
	limit, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	if limit != MaxImageAttachments {
		t.Fatalf("GUI image limit = %d, runtime limit = %d", limit, MaxImageAttachments)
	}
}

func TestRuntimeRejectsAttachmentsWithoutImageToolCapability(t *testing.T) {
	rt := New(&testBot{}, Services{})
	_, err := rt.StartRun(context.Background(), Input{
		Text:   "inspect [Image #1]",
		Images: []ImageAttachment{{Label: "[Image #1]", Path: "/tmp/paste.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "image attachments require") {
		t.Fatalf("StartRun error = %v", err)
	}
}

func TestStartRunSerializesImageCapabilityWithRuntimeMutation(t *testing.T) {
	capabilityEntered := make(chan struct{})
	releaseCapability := make(chan struct{})
	releaseRun := make(chan struct{})
	var applyCalled atomic.Bool
	bot := &testBot{run: func(string, RunHost) (string, error) {
		<-releaseRun
		return "", nil
	}}
	services := testBotServices(bot)
	services.ImageAttachmentsEnabled = func() bool {
		close(capabilityEntered)
		<-releaseCapability
		return true
	}
	services.SetReasoningEffort = func(string) error {
		applyCalled.Store(true)
		return nil
	}
	rt := New(bot, services)
	defer func() {
		close(releaseRun)
		_ = rt.Close()
	}()
	startDone := make(chan error, 1)
	go func() {
		_, err := rt.StartRun(context.Background(), Input{
			Text:   "inspect [Image #1]",
			Images: []ImageAttachment{{Label: "[Image #1]", Path: "/tmp/paste.png"}},
		})
		startDone <- err
	}()
	<-capabilityEntered
	applyDone := make(chan error, 1)
	applyAttempted := make(chan struct{})
	go func() {
		close(applyAttempted)
		err := rt.SetReasoningEffort("high")
		applyDone <- err
	}()
	<-applyAttempted
	mutationCrossed := false
	var applyErr error
	select {
	case applyErr = <-applyDone:
		mutationCrossed = true
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseCapability)
	if mutationCrossed {
		t.Fatal("config mutation crossed the in-flight image capability check")
	}
	if err := <-startDone; err != nil {
		t.Fatal(err)
	}
	if !mutationCrossed {
		applyErr = <-applyDone
	}
	if applyErr == nil || !strings.Contains(applyErr.Error(), "run") {
		t.Fatalf("ApplyConfig error = %v, want active-run rejection", applyErr)
	}
	if applyCalled.Load() {
		t.Fatal("config service ran after image capability was accepted")
	}
}

func TestInputWithImageAttachmentsKeepsVisibleTextSeparate(t *testing.T) {
	visible := "分析这张图 [Image #1]"
	got := InputWithImageAttachments(visible, []ImageAttachment{{
		Label: "[Image #1]",
		Path:  "/tmp/paste.png",
	}})
	if !strings.Contains(got, `"label":"[Image #1]"`) || !strings.Contains(got, `"path":"/tmp/paste.png"`) {
		t.Fatalf("attachment metadata missing: %q", got)
	}
	if restored := VisibleInputText(got); restored != visible {
		t.Fatalf("visible text = %q, want %q", restored, visible)
	}
}

func TestInputWithImageAttachmentsIgnoresIncompleteEntries(t *testing.T) {
	const text = "hello"
	got := InputWithImageAttachments(text, []ImageAttachment{{Label: "[Image #1]"}})
	if got != text {
		t.Fatalf("invalid attachment changed input: %q", got)
	}
}

func TestVisibleInputTextDoesNotStripUserAuthoredMarker(t *testing.T) {
	text := "example\n\n<image_attachments>\nnot runtime metadata\n</image_attachments>"
	if got := VisibleInputText(text); got != text {
		t.Fatalf("user-authored content was stripped: %q", got)
	}
}

func TestVisibleInputTextDoesNotStripUserAuthoredEnvelope(t *testing.T) {
	text := "example [Image #1]" + imageAttachmentsStart + `{"images":[{"label":"[Image #1]","path":"/tmp/example.png"}],"instruction":"user example"}` + imageAttachmentsEnd
	if got := VisibleInputText(text); got != text {
		t.Fatalf("user-authored envelope was stripped: %q", got)
	}
}
