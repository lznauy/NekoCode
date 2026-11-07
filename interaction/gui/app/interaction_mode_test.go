package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"nekocode/bot/config"
	botcore "nekocode/bot/core"
	"nekocode/mode"
	controlruntime "nekocode/runtime"
	"nekocode/runtime/standard"
)

func TestPlanDraftSurvivesClipboardImageAndNextRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "read request", 400)
			return
		}
		if !bytes.Contains(body, []byte("PLAN MODE")) {
			t.Error("clipboard initialization removed the Plan runtime policy")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Read-only plan\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	vision := true
	if err := config.Save(config.Config{Active: "fixture/default", Providers: []config.ProviderConfig{{Name: "fixture", Protocol: "openai", BaseURL: server.URL + "/v1", APIKey: "fixture", Models: []config.ModelDefinition{{Name: "default", Model: "deepseek-v4-flash", Vision: &vision}}}}}); err != nil {
		t.Fatal(err)
	}
	bot, err := botcore.NewAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rt := standard.FromBot(bot)
	defer rt.Close()
	app := &App{ctx: t.Context(), rt: rt}
	original := rt.CurrentSessionID()
	if _, err := app.SetInteractionMode("plan"); err != nil {
		t.Fatal(err)
	}
	image, err := app.SaveClipboardImage("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jaz0AAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	if rt.CurrentSessionID() != original || filepath.Base(filepath.Dir(image)) != original {
		t.Fatal("mode selection or clipboard initialization replaced the draft identity")
	}
	if app.InteractionMode().Selected != mode.Plan {
		t.Fatal("clipboard initialization silently left Plan")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	id, err := rt.StartRun(ctx, controlruntime.Input{Text: "Inspect [Image #1] and return a plan", Source: controlruntime.SourceRef{Kind: "gui"}, Images: []controlruntime.ImageAttachment{{Label: "[Image #1]", Path: image}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, id); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := rt.LookupRun(id)
	if !ok || snapshot.Status != controlruntime.RunDone || snapshot.InteractionMode.ID != mode.Plan || snapshot.InteractionMode.ToolPolicy != mode.ReadOnlyTools {
		t.Fatalf("next run lost Plan ceiling: %+v", snapshot)
	}
	if app.InteractionMode().Selected != mode.Plan {
		t.Fatal("run cleanup removed the saved Plan preference")
	}
}
