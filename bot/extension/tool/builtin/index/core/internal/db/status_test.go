package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStatusPersistsAndCountsStoredFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	d, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	for path, lang := range map[string]string{"a.go": "go", "b.go": "go", "c.py": "python"} {
		if err := d.SaveFile(path, "hash", lang); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.RecordSync(); err != nil {
		t.Fatal(err)
	}
	before, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if before.Files != 3 || before.Languages["go"] != 2 || before.Languages["python"] != 1 {
		t.Fatalf("%+v", before)
	}
	if _, err := time.Parse(time.RFC3339Nano, before.LastSync); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	after, err := d.Status()
	if err != nil || after.LastSync != before.LastSync || after.Files != before.Files {
		t.Fatalf("%+v %v", after, err)
	}
}
