package session

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryReviewBadIndexEntryDoesNotAlterTranscript(t *testing.T) {
	s := seedIndexedHistory(t, 3)
	if _, err := s.Count(3, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, transcriptFileName)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := os.OpenFile(filepath.Join(s.dir, "transcript.idx"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Damage only the first entry CRC; leave the source and cached stat intact.
	if _, err := idx.WriteAt([]byte{0, 0, 0, 0}, historyHeaderSize+24); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Page(0, 100, HistoryPageBytes, 3); err == nil {
		t.Fatal("accepted damaged index entry")
	}
	if _, err := s.ReadRange(1, 3); err == nil {
		t.Fatal("accepted damaged index range")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("read changed source transcript")
	}
}

func TestHistoryReviewConfirmedCorruptionIsNotRepaired(t *testing.T) {
	s := seedIndexedHistory(t, 3)
	if _, err := s.Count(3, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, transcriptFileName)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want = append(want[:len(want)-6], []byte("bad\n")...)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Count(3, true); err == nil {
		t.Fatal("accepted damaged confirmed record")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("repair truncated confirmed source")
	}
}
