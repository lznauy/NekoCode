package contextmgr

import (
	"encoding/json"
	"testing"

	"nekocode/bot/provider/types"
)

func TestAddUserPersistsImagesThroughSnapshotRoundTrip(t *testing.T) {
	m := New(Config{SystemPrompt: "test", ContextWindow: 128000})
	images := []types.MessageImage{
		{Path: "/tmp/images/sess/paste-1.png", MIME: "image/png", Width: 1024, Height: 768},
		{Path: "/tmp/images/sess/paste-2.jpg", MIME: "image/jpeg", Width: 1568, Height: 1568},
	}
	m.AddUser("look [Image #1] and [Image #2]", images)

	snapshot := m.Snapshot()
	if len(snapshot.Messages) != 1 || len(snapshot.Messages[0].Images) != 2 {
		t.Fatalf("snapshot messages = %+v", snapshot.Messages)
	}
	if snapshot.Messages[0].Images[0].Path != images[0].Path || snapshot.Messages[0].Images[0].MIME != "image/png" {
		t.Fatalf("snapshot image metadata = %+v", snapshot.Messages[0].Images[0])
	}

	// The session file is JSON; images must survive serialization and reload.
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded ManagerSnapshot
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatal(err)
	}
	m2 := New(Config{SystemPrompt: "test", ContextWindow: 128000})
	m2.Restore(reloaded)
	restored := m2.Snapshot()
	if len(restored.Messages) != 1 || len(restored.Messages[0].Images) != 2 {
		t.Fatalf("restored messages = %+v", restored.Messages)
	}
	if restored.Messages[0].Images[1].Width != 1568 || restored.Messages[0].Images[1].Height != 1568 {
		t.Fatalf("restored dimensions = %+v", restored.Messages[0].Images[1])
	}
}

func TestAddUserChargesImageTokensOnlyOnVisionModels(t *testing.T) {
	images := []types.MessageImage{{Path: "/tmp/a.png", Width: 800, Height: 600}}
	nonVision := New(Config{SystemPrompt: "test", ContextWindow: 128000})
	before := nonVision.Status().Tokens
	nonVision.AddUser("look [Image #1]", images)
	if delta := nonVision.Status().Tokens - before; delta >= 800*600/750 {
		t.Fatalf("non-vision token delta = %d, image tokens must not be charged", delta)
	}

	vision := New(Config{SystemPrompt: "test", ContextWindow: 128000})
	vision.ConfigureModel(ModelContext{Vision: true})
	before = vision.Status().Tokens
	vision.AddUser("look [Image #1]", images)
	// The image estimate (already a token count) must bypass AddNew's
	// chars→tokens conversion; routing it through AddNew would undercharge
	// 640 → ~160 and delay compaction until the request overflows.
	if delta := vision.Status().Tokens - before; delta < 800*600/750 {
		t.Fatalf("vision token delta = %d, want >= %d full image tokens", delta, 800*600/750)
	}
}
