package connect

import (
	"encoding/json"
	"fmt"
	"os"
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
	files, err := filepath.Glob(filepath.Join(filepath.Dir(store.path), ".connect-*.tmp"))
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
