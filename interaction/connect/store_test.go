package connect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// TestFileStorePreservesOtherSections is the regression test for the
// historical bug where telegram's saveConfig rewrote the whole file and
// dropped every other connector's section.
func TestFileStorePreservesOtherSections(t *testing.T) {
	store := &FileStore{path: filepath.Join(t.TempDir(), "connect.json")}

	type telegramCfg struct {
		ActiveProfile string `json:"active_profile,omitempty"`
	}
	type feishuCfg struct {
		AppID string `json:"app_id,omitempty"`
	}

	if err := store.Save("telegram", telegramCfg{ActiveProfile: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("feishu", feishuCfg{AppID: "cli_a"}); err != nil {
		t.Fatal(err)
	}

	var tg telegramCfg
	if err := store.Load("telegram", &tg); err != nil {
		t.Fatal(err)
	}
	if tg.ActiveProfile != "main" {
		t.Fatalf("telegram section clobbered by feishu save: %+v", tg)
	}

	// And the reverse: saving telegram must keep the feishu section.
	if err := store.Save("telegram", telegramCfg{ActiveProfile: "alt"}); err != nil {
		t.Fatal(err)
	}
	var fs feishuCfg
	if err := store.Load("feishu", &fs); err != nil {
		t.Fatal(err)
	}
	if fs.AppID != "cli_a" {
		t.Fatalf("feishu section clobbered by telegram save: %+v", fs)
	}
	if tg.ActiveProfile = ""; store.Load("telegram", &tg) != nil || tg.ActiveProfile != "alt" {
		t.Fatalf("telegram section = %+v, want alt", tg)
	}
}

func TestFileStoreLoadMissing(t *testing.T) {
	store := &FileStore{path: filepath.Join(t.TempDir(), "connect.json")}
	var out struct {
		X string `json:"x"`
	}
	if err := store.Load("nope", &out); err != nil {
		t.Fatalf("missing file should load as zero, got %v", err)
	}

	// Unknown section in an existing file also loads as zero.
	if err := store.Save("other", map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Load("nope", &out); err != nil {
		t.Fatalf("missing section should load as zero, got %v", err)
	}
}

func TestFileStoreRawRoundTrip(t *testing.T) {
	store := &FileStore{path: filepath.Join(t.TempDir(), "connect.json")}
	if err := store.Save("a", map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("b", map[string]int{"n": 2}); err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("sections = %d, want 2", len(raw))
	}
}

func TestFileStoreConcurrentInstancesPreserveSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connect.json")
	const count = 32
	start := make(chan struct{})
	failures := make(chan error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			store := &FileStore{path: path}
			failures <- store.Save(fmt.Sprintf("connector-%d", i), i)
		}()
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := range count {
		var got int
		if err := (&FileStore{path: path}).Load(fmt.Sprintf("connector-%d", i), &got); err != nil {
			t.Fatal(err)
		}
		if got != i {
			t.Fatalf("section %d = %d", i, got)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(data, &sections); err != nil {
		t.Fatal(err)
	}
	if len(sections) != count {
		t.Fatalf("got %d sections, want %d", len(sections), count)
	}
}

func TestFileStorePersistsSectionsAcrossProcesses(t *testing.T) {
	const entries = 12
	if section := os.Getenv("NEKOCODE_TEST_CONNECT_SECTION"); section != "" {
		store := &FileStore{path: os.Getenv("NEKOCODE_TEST_CONNECT_PATH")}
		for i := range entries {
			if err := store.Save(fmt.Sprintf("%s-%d", section, i), true); err != nil {
				t.Fatal(err)
			}
		}
		return
	}

	path := filepath.Join(t.TempDir(), "connect.json")
	const writers = 4
	var wg sync.WaitGroup
	for n := range writers {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestFileStorePersistsSectionsAcrossProcesses$")
			cmd.Env = append(os.Environ(), "NEKOCODE_TEST_CONNECT_PATH="+path, fmt.Sprintf("NEKOCODE_TEST_CONNECT_SECTION=writer%d", n))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("writer %d: %v: %s", n, err, output)
			}
		})
	}
	wg.Wait()
	sections, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]bool
	if err := json.Unmarshal(sections, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved) != writers*entries {
		t.Fatalf("saved %d of %d sections", len(saved), writers*entries)
	}
}

func TestFileStoreRejectsInvalidExistingJSON(t *testing.T) {
	for _, original := range []string{"broken json", "{", "[]", "true"} {
		t.Run(original, func(t *testing.T) {
			store := &FileStore{path: filepath.Join(t.TempDir(), "connect.json")}
			if err := os.WriteFile(store.path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if err := store.Save("telegram", true); err == nil {
				t.Fatal("invalid config was overwritten")
			}
			got, err := os.ReadFile(store.path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != original {
				t.Fatalf("config changed to %s", got)
			}
		})
	}
}

func TestFileStoreSaveNullAndPrivatePermissions(t *testing.T) {
	store := &FileStore{path: filepath.Join(t.TempDir(), "connect.json")}
	if err := os.WriteFile(store.path, []byte("null"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("telegram", true); err != nil {
		t.Fatal(err)
	}
	var got bool
	if err := store.Load("telegram", &got); err != nil || !got {
		t.Fatalf("load = %v, %v", got, err)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(store.path), ".connect.json-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("temporary files remain: %v", files)
	}
}

func TestFileStoreReadFailurePreservesTarget(t *testing.T) {
	store := &FileStore{path: t.TempDir()}
	if err := store.Save("telegram", true); err == nil {
		t.Fatal("save to directory succeeded")
	}
	info, err := os.Stat(store.path)
	if err != nil || !info.IsDir() {
		t.Fatalf("target changed: %v", err)
	}
}

func TestFileStoreReadersSeeCompleteJSON(t *testing.T) {
	store := &FileStore{path: filepath.Join(t.TempDir(), "connect.json")}
	if err := store.Save("telegram", 0); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		for {
			select {
			case <-done:
				result <- nil
				return
			default:
			}
			var got int
			if err := store.Load("telegram", &got); err != nil {
				result <- err
				return
			}
		}
	}()
	for i := range 32 {
		if err := store.Save("telegram", i); err != nil {
			close(done)
			<-result
			t.Fatal(err)
		}
	}
	close(done)
	if err := <-result; err != nil {
		t.Fatalf("reader saw incomplete config: %v", err)
	}
}

func TestVersionedConfigComparesOnlyItsSectionUnderLock(t *testing.T) {
	store := &FileStore{path: filepath.Join(t.TempDir(), "connect.json")}
	// Handwritten config from before versions existed. Saving another section reformats the file.
	if err := os.WriteFile(store.path, []byte(`{"legacy": { "app_id" : "x&y" }}`), 0600); err != nil {
		t.Fatal(err)
	}
	var config map[string]string
	version, err := store.LoadVersioned("legacy", &config)
	if err != nil || config["app_id"] != "x&y" {
		t.Fatalf("load legacy: %v, %v", config, err)
	}
	if err := store.SaveVersioned("other", map[string]string{"app_id": "other"}, nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"first", "second"} {
		wg.Go(func() { results <- store.SaveVersioned("legacy", map[string]string{"app_id": id}, version) })
	}
	wg.Wait()
	first, second := <-results, <-results
	if !(first == nil && errors.Is(second, ErrConfigChanged) || second == nil && errors.Is(first, ErrConfigChanged)) {
		t.Fatalf("expected one commit and one conflict: %v, %v", first, second)
	}
	version, err = store.LoadVersioned("legacy", &config)
	if err != nil {
		t.Fatal(err)
	}
	config["app_id"] = "manually replaced"
	if err := store.Save("legacy", config); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveVersioned("legacy", config, version); !errors.Is(err, ErrConfigChanged) {
		t.Fatalf("manual edit with the same revision was overwritten: %v", err)
	}
}
