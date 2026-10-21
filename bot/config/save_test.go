package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveDoesNotMutateInput(t *testing.T) {
	for _, valid := range []bool{true, false} {
		name := "success"
		if !valid {
			name = "validation_failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			cfg := Config{Models: []ModelConfig{{Name: " default ", Provider: " openai ", Model: " model "}}}
			if !valid {
				cfg.Models = append(cfg.Models, ModelConfig{Name: "broken"})
			}
			before := cfg.Clone()
			err := Save(cfg)
			if (err == nil) != valid {
				t.Fatalf("unexpected save result: %v", err)
			}
			if !reflect.DeepEqual(cfg, before) {
				t.Fatal("Save mutated caller-owned configuration")
			}
		})
	}
}

func TestSaveReplacesFileWithoutChangingOpenReaders(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "regular"
		if linked {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			cfg := Config{Active: "first", Models: []ModelConfig{
				{Name: "first", Provider: "openai", Model: "first"},
				{Name: "second", Provider: "openai", Model: "second"},
			}}
			if err := Save(cfg); err != nil {
				t.Fatal(err)
			}
			if linked {
				target := filepath.Join(t.TempDir(), "settings.json")
				if err := os.Rename(Path(), target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, Path()); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(Path())
			if err != nil {
				t.Fatal(err)
			}
			reader, err := os.Open(Path())
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			cfg.Active = "second"
			if err := Save(cfg); err != nil {
				t.Fatal(err)
			}
			old, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(old, before) {
				t.Error("saving changed a reader's existing configuration snapshot")
			}
			saved, err := Load()
			if err != nil || saved.Active != "second" {
				t.Fatalf("new configuration was not installed: %v", err)
			}
			info, err := os.Stat(Path())
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("configuration permissions are not private: %v", err)
			}
			if linked {
				info, err := os.Lstat(Path())
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("saving replaced the configuration symlink")
				}
			}
		})
	}
}

func TestSaveFollowsRelativeSymlinkToNewTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nested/settings.json", Path()); err != nil {
		t.Fatal(err)
	}
	if err := Save(Default); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(Path())
	if err != nil || target != "nested/settings.json" {
		t.Fatalf("configuration link changed: %q %v", target, err)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveCleansTemporaryFileOnReplacementFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(Path(), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(Path(), "keep")
	if err := os.WriteFile(marker, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Save(Default); err == nil {
		t.Fatal("save over directory unexpectedly succeeded")
	}
	entries, err := os.ReadDir(filepath.Dir(Path()))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatal("failed save leaked temporary files")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "original" {
		t.Fatal("failed save damaged existing target")
	}
}

func TestSaveRejectsSymlinkCycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("config.json", Path()); err != nil {
		t.Fatal(err)
	}
	if err := Save(Default); err == nil {
		t.Fatal("cyclic configuration link accepted")
	}
}

func TestSaveResolvesDirectoryLinksBeforeParentTraversal(t *testing.T) {
	for _, linkedHome := range []bool{false, true} {
		name := "target_directory_link"
		if linkedHome {
			name = "configuration_directory_link"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			actualDir := t.TempDir()
			actualPath := filepath.Join(actualDir, "settings.json")
			cfg := Config{Active: "first", Models: []ModelConfig{{Name: "first", Provider: "openai", Model: "first"}, {Name: "second", Provider: "openai", Model: "second"}}}
			if err := Save(cfg); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(Path(), actualPath); err != nil {
				t.Fatal(err)
			}
			nested := filepath.Join(actualDir, "nested")
			if err := os.Mkdir(nested, 0700); err != nil {
				t.Fatal(err)
			}
			target := "hop/../settings.json"
			if linkedHome {
				if err := os.Remove(filepath.Dir(Path())); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(nested, filepath.Dir(Path())); err != nil {
					t.Fatal(err)
				}
				target = "../settings.json"
			} else {
				if err := os.Symlink(nested, filepath.Join(filepath.Dir(Path()), "hop")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, Path()); err != nil {
				t.Fatal(err)
			}
			cfg.Active = "second"
			if err := Save(cfg); err != nil {
				t.Fatal(err)
			}
			got, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if got.Active != "second" {
				t.Fatalf("saved configuration differs from linked target: active=%q", got.Active)
			}
		})
	}
}
