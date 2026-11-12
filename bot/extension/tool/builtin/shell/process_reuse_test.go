package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/bot/extension/tool/runtime/sandbox"
)

func TestAnonymousConcurrentLaunchesReuseOnePhysicalProcess(t *testing.T) {
	registry := newTaskRegistry()
	defer registry.cancel()
	defer registry.stopAll()
	dir := t.TempDir()
	count := filepath.Join(dir, "launches")
	command := "printf 'started\\n' >> '" + strings.ReplaceAll(count, "'", "'\\''") + "'; sleep 3"
	req := startRequest{owner: "session", command: command, directory: dir, host: true, sampleWait: 100 * time.Millisecond}
	var wg sync.WaitGroup
	results := make(chan processResult, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, err := registry.start(t.Context(), req); results <- r; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	name := ""
	reused := 0
	for result := range results {
		if name == "" {
			name = result.info.name
		}
		if result.info.name != name {
			t.Fatal("duplicate command got a different process")
		}
		if result.reason == "reused_running" {
			reused++
			if text := formatProcessResult(result); !strings.Contains(text, "no new command was started") {
				t.Fatal("reuse was not explicit")
			}
		}
	}
	if reused != 7 {
		t.Fatalf("expected seven reuses, got %d", reused)
	}
	data, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "started\n" {
		t.Fatalf("command launched more than once: %q", data)
	}
}

func TestLaunchIdentityIncludesScopePermissionsEnvironmentAndLifetime(t *testing.T) {
	base := startRequest{owner: "A", directory: "/workspace", command: "sleep 3", host: true, timeout: time.Minute}
	key := base.identity()
	for _, change := range []func(*startRequest){
		func(r *startRequest) { r.owner = "B" }, func(r *startRequest) { r.directory = "/other" },
		func(r *startRequest) { r.command = "sleep 4" }, func(r *startRequest) { r.host = false },
		func(r *startRequest) { r.profile = sandbox.Profile{Network: true} },
		func(r *startRequest) { r.profile = sandbox.Profile{OutboundDomains: []string{}} },
		func(r *startRequest) { r.profile = sandbox.Profile{WritePaths: []string{"/other"}} },
		func(r *startRequest) { r.timeout = 2 * time.Minute },
	} {
		req := base
		change(&req)
		if req.identity() == key {
			t.Fatal("different launch environment shares identity")
		}
	}
	t.Setenv("NEKOCODE_PROCESS_TEST_IDENTITY", "different")
	if base.identity() == key {
		t.Fatal("changed inherited environment reused identity")
	}
	t.Setenv("NEKOCODE_PROCESS_TEST_IDENTITY", string([]byte{0xff}))
	invalidKey := base.identity()
	t.Setenv("NEKOCODE_PROCESS_TEST_IDENTITY", string([]byte{0xfe}))
	if base.identity() == invalidKey {
		t.Fatal("invalid UTF-8 environment bytes were collapsed")
	}
}

func TestNamedCopiesAndEndedProcessAllowNewLaunches(t *testing.T) {
	registry := newTaskRegistry()
	defer registry.cancel()
	defer registry.stopAll()
	req := startRequest{owner: "A", directory: t.TempDir(), command: "sleep 3", host: true, sampleWait: time.Millisecond}
	first, err := registry.start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.name = "intentional-copy"
	second, err := registry.start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.info.name == second.info.name {
		t.Fatal("explicit parallel copy was coalesced")
	}
	if _, err := registry.stop("A", first.info.name); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.stop("A", second.info.name); err != nil {
		t.Fatal(err)
	}
	req.name = ""
	third, err := registry.start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if third.reason == "reused_running" {
		t.Fatal("terminal processes prevented retry")
	}
}

func TestAdoptingStartupProcessSurvivesFirstCallerCancellation(t *testing.T) {
	registry := newTaskRegistry()
	defer registry.cancel()
	defer registry.stopAll()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := startRequest{owner: "A", directory: t.TempDir(), command: "sleep 5", host: true, sampleWait: time.Minute}
	finished := make(chan error, 1)
	go func() { _, err := registry.start(ctx, req); finished <- err }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		registry.mu.Lock()
		registered := len(registry.tasks) > 0
		registry.mu.Unlock()
		if registered {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("startup did not register process")
		}
	}
	adopted, err := registry.start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.reason != "reused_running" {
		t.Fatal("second caller did not adopt startup")
	}
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("expected cancelled observation")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled startup did not return")
	}
	task, err := registry.task("A", adopted.info.name)
	if err != nil || !task.isRunning() {
		t.Fatal("first caller cancellation stopped adopted process")
	}
}

func TestAnonymousLaunchDoesNotReuseAnotherOwner(t *testing.T) {
	registry := newTaskRegistry()
	defer registry.cancel()
	defer registry.stopAll()
	req := startRequest{owner: "A", directory: t.TempDir(), command: "sleep 3", host: true, sampleWait: time.Millisecond}
	first, err := registry.start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.owner = "B"
	second, err := registry.start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.info.name == second.info.name || second.reason == "reused_running" {
		t.Fatal("another owner reused a live process")
	}
	if _, err := registry.task("B", first.info.name); err == nil {
		t.Fatal("another owner can inspect reused process")
	}
}
