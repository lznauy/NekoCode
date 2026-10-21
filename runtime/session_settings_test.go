package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSessionSettingsRollbackRestoresBothModelProfiles(t *testing.T) {
	for _, failModel := range []bool{false, true} {
		t.Run(map[bool]string{false: "reasoning", true: "model"}[failModel], func(t *testing.T) {
			active := "old"
			efforts := map[string]string{"old": "low", "new": "medium"}
			full := false
			failure := errors.New("injected failure")
			r := New(&testBot{}, ServiceGroups{
				Models: ModelsServices{
					ModelOptions: func() ([]ModelOption, string) {
						return []ModelOption{{Name: "old", ReasoningEffort: efforts["old"]}, {Name: "new", ReasoningEffort: efforts["new"]}}, active
					},
					CurrentModel: func() ModelSelection { return ModelSelection{Model: active, ReasoningEffort: efforts[active]} },
					SwitchSessionModel: func(name string) (ModelSelection, error) {
						active = name
						if failModel && name == "new" {
							return ModelSelection{}, failure
						}
						return ModelSelection{Model: name}, nil
					},
					SetSessionReasoning: func(effort string) error {
						efforts[active] = effort
						if effort == "invalid" {
							return failure
						}
						return nil
					},
				},
				Permissions: PermissionsServices{
					PermissionMode: func() string {
						if full {
							return "full"
						}
						return "manual"
					},
					SetFullAccess: func(on bool) { full = on },
				},
			})
			defer r.Close()
			err := r.ApplySessionSettings(SessionSettings{Model: "new", ReasoningEffort: "invalid", FullAccess: true})
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v", err)
			}
			if active != "old" || efforts["old"] != "low" || efforts["new"] != "medium" || full {
				t.Fatalf("state: %s %v %v", active, efforts, full)
			}
			if failModel && !strings.Contains(err.Error(), "rollback session settings") {
				t.Fatalf("rollback error lost: %v", err)
			}
		})
	}
}

func TestSessionSettingsRejectUnsupportedAndBusy(t *testing.T) {
	r := New(&testBot{}, ServiceGroups{})
	defer r.Close()
	if err := r.ApplySessionSettings(SessionSettings{}); err == nil {
		t.Fatal("unsupported operation accepted")
	}
	// Install lightweight functions to exercise the common mutation gate.
	r.services.Models.ModelOptions = func() ([]ModelOption, string) { t.Fatal("busy operation read services"); return nil, "" }
	r.services.Models.CurrentModel = func() ModelSelection { return ModelSelection{} }
	r.services.Permissions.PermissionMode = func() string { return "manual" }
	r.services.Models.SwitchSessionModel = func(string) (ModelSelection, error) { return ModelSelection{}, nil }
	r.services.Models.SetSessionReasoning = func(string) error { return nil }
	r.services.Permissions.SetFullAccess = func(bool) {}
	r.status = RunRunning
	if err := r.ApplySessionSettings(SessionSettings{}); err == nil {
		t.Fatal("busy operation accepted")
	}
	r.status = RunIdle
}

func TestSessionSettingsExcludeRunStartup(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	r := New(&testBot{}, ServiceGroups{
		Models: ModelsServices{
			ModelOptions:        func() ([]ModelOption, string) { return nil, "old" },
			CurrentModel:        func() ModelSelection { return ModelSelection{} },
			SwitchSessionModel:  func(string) (ModelSelection, error) { close(entered); <-release; return ModelSelection{}, nil },
			SetSessionReasoning: func(string) error { return nil },
		},
		Permissions: PermissionsServices{
			PermissionMode: func() string { return "manual" },
			SetFullAccess:  func(bool) {},
		},
	})
	defer r.Close()
	changed := make(chan error, 1)
	go func() { changed <- r.ApplySessionSettings(SessionSettings{Model: "new"}) }()
	<-entered
	started := make(chan error, 1)
	go func() {
		id, err := r.StartRun(context.Background(), Input{Text: "run"})
		if err == nil {
			err = r.WaitRun(context.Background(), id)
		}
		started <- err
	}()
	select {
	case err := <-started:
		close(release)
		t.Fatalf("run bypassed mutation: %v", err)
	default:
	}
	close(release)
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	if err := <-started; err != nil {
		t.Fatal(err)
	}
}
