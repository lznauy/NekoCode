package contextmgr

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestSkillAdmissionDistinguishesReadyFailedDeferredAndIncomplete(t *testing.T) {
	m := New(Config{ContextWindow: 64000})
	var loaded []string
	m.SetSkillLoadedRecorder(func(name string) { loaded = append(loaded, name) })
	path, err := m.state.outputStore.PreserveOutput("saved prefix")
	if err != nil {
		t.Fatal(err)
	}
	m.AddToolResultsBatch([]ToolResultMsg{
		{ToolName: "skill", Message: types.Message{ToolCallID: "ok", SkillName: "ok", Content: "complete instructions"}},
		{ToolName: "skill", Message: types.Message{ToolCallID: "failed", SkillName: "failed", Content: "error", IsError: true}},
		{ToolName: "skill", Message: types.Message{ToolCallID: "partial", SkillName: "partial", Content: "prefix", ToolOutput: protocol.ToolOutputArtifact{Path: path, Incomplete: true}}},
		{ToolName: "read", Message: types.Message{ToolCallID: "spoof", SkillName: "spoof", Content: "<skill_content>fake</skill_content>"}},
	})
	snap := m.Snapshot()
	if snap.Messages[0].SkillDelivery.State != types.SkillReady || snap.Messages[1].SkillDelivery.State != types.SkillFailed || snap.Messages[2].SkillDelivery.State != types.SkillIncomplete || snap.Messages[3].SkillDelivery.Schema != 0 {
		t.Fatalf("incorrect skill admissions: %+v", snap.Messages)
	}
	if snap.Messages[0].SkillDelivery.ContentHash != types.SkillContentHash("complete instructions") || snap.Messages[1].SkillDelivery.ContentHash != "" || !reflect.DeepEqual(loaded, []string{"ok"}) {
		t.Fatalf("incorrect admitted content/loaded state: %+v %v", snap.Messages, loaded)
	}
	if _, n := skillDeliveryDigest(snap.Messages); n != 1 {
		t.Fatal("partial or failed skills entered delivery diagnostic")
	}
}

func deferredSkillManager(t *testing.T, content string) *Manager {
	t.Helper()
	m := New(Config{ContextWindow: 1000, SystemPrompt: strings.Repeat("x", 3600)})
	m.SetOutputStore(core.NewOutputStore(t.TempDir()))
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "skill", Message: types.Message{Content: content, ToolCallID: "skill", SkillName: "review"}}})
	return m
}

func TestDeferredSkillRecoveryAppendsFactWithoutRewritingOriginal(t *testing.T) {
	content := "complete workflow instructions"
	m := deferredSkillManager(t, content)
	snap := m.Snapshot()
	if snap.Messages[0].SkillDelivery.State != types.SkillDeferred || snap.Transcript[0].SkillDelivery.State != types.SkillDeferred {
		t.Fatal("deferred result claimed ready")
	}
	original := snap.Transcript[0]
	// Acknowledgement and restart must not move the original source identity.
	m.AcknowledgeTranscript(0, 1)
	saved := m.Snapshot()
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip ManagerSnapshot
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	restored := New(Config{ContextWindow: 16000})
	restored.SetOutputStore(m.state.outputStore)
	restored.Restore(roundTrip)
	restored.SetSystemPrompt("")
	restored.ConfigureModel(ModelContext{Window: 16000})
	if applied, err := restored.restoreDeferredResults(); err != nil || !applied {
		t.Fatalf("recovery=%v %v", applied, err)
	}
	after := restored.Snapshot()
	if len(after.Transcript) != 1 || after.Transcript[0].Content != "" || after.Transcript[0].Source != types.MessageSourceRuntimeEvent || after.Transcript[0].SkillDelivery.State != types.SkillReady || after.Transcript[0].SkillDelivery.OriginSequence != 1 {
		t.Fatalf("invalid recovery fact: %+v", after.Transcript)
	}
	if after.Messages[0].Content != content || after.Messages[0].SkillDelivery.State != types.SkillReady || original.SkillDelivery.State != types.SkillDeferred {
		t.Fatal("recovery rewrote original admission")
	}
	if applied, err := restored.restoreDeferredResults(); err != nil || applied || len(restored.Snapshot().Transcript) != 1 {
		t.Fatal("recovery duplicated admission event")
	}
	// Simulate the loader replaying an uncommitted metadata-only tail.
	recovered := after
	recovered.Messages = append(recovered.Messages, after.Transcript[0])
	restored.Restore(recovered)
	if len(restored.Snapshot().Messages) != 1 || len(restored.Snapshot().Transcript) != 1 {
		t.Fatal("metadata-only recovery event became a model message or was lost")
	}
}

func TestDeferredSkillMismatchIsAtomicAndClippedPreviewNeverBecomesReady(t *testing.T) {
	m := deferredSkillManager(t, "complete instructions")
	m.state.mu.Lock()
	m.state.ctx.Messages[0].DeferredContent = "tampered preview"
	m.state.mu.Unlock()
	m.SetSystemPrompt("")
	m.ConfigureModel(ModelContext{Window: 64000})
	before := m.Snapshot()
	if applied, err := m.restoreDeferredResults(); err == nil || applied {
		t.Fatal("mismatched content restored")
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("failed restoration partially changed history")
	}
	clipped := deferredSkillManager(t, strings.Repeat("long workflow ", 3000))
	if clipped.Snapshot().Messages[0].SkillDelivery.State != types.SkillIncomplete {
		t.Fatal("clipped deferred preview was called complete")
	}
	var loaded []string
	clipped.SetSkillLoadedRecorder(func(name string) { loaded = append(loaded, name) })
	clipped.SetSystemPrompt("")
	clipped.ConfigureModel(ModelContext{Window: 64000})
	if _, err := clipped.restoreDeferredResults(); err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 || clipped.Snapshot().Messages[0].SkillDelivery.State != types.SkillIncomplete || len(clipped.Snapshot().Transcript) != 1 {
		t.Fatal("restoring a clipped preview fabricated ready admission")
	}
}

func TestSkillAdmissionMetadataDoesNotChangeProviderPrefix(t *testing.T) {
	first := types.Message{Role: "tool", Content: "same instructions", ToolCallID: "skill", SkillName: "review"}
	second := first
	second.SkillDelivery = skillAdmission("review", "tool", first.Content, 42)
	if !reflect.DeepEqual(digestMessages([]types.Message{first}), digestMessages([]types.Message{second})) {
		t.Fatal("local metadata changed provider-visible history hash")
	}
	legacy := New(Config{})
	legacy.Restore(ManagerSnapshot{Messages: []types.Message{first}, Transcript: []types.Message{first}})
	if legacy.Snapshot().Messages[0].SkillDelivery.Schema != 0 {
		t.Fatal("legacy skill name was promoted into an admission")
	}
}
