package editcore

import (
	"testing"
)

// ---------------------------------------------------------------------------
// hash.go tests
// ---------------------------------------------------------------------------

func TestComputeFileHash(t *testing.T) {
	hash := ComputeFileHash("hello\nworld\n")
	if len(hash) != 8 {
		t.Fatalf("expected 8-char hash, got %q", hash)
	}
	hash2 := ComputeFileHash("hello\nworld\n")
	if hash != hash2 {
		t.Fatalf("same content should produce same hash: %q vs %q", hash, hash2)
	}
	hash3 := ComputeFileHash("hello\nworld!\n")
	if hash == hash3 {
		t.Fatalf("different content should produce different hash: %q vs %q", hash, hash3)
	}
}

func TestComputeFileHash_CRLF(t *testing.T) {
	hashLF := ComputeFileHash("hello\nworld\n")
	hashCRLF := ComputeFileHash("hello\r\nworld\r\n")
	if hashLF != hashCRLF {
		t.Fatalf("CRLF/LF should produce same hash: %q vs %q", hashLF, hashCRLF)
	}
}

func TestComputeFileHash_TrailingWhitespace(t *testing.T) {
	hash1 := ComputeFileHash("hello\nworld\n")
	hash2 := ComputeFileHash("hello  \nworld\t\n")
	if hash1 != hash2 {
		t.Fatalf("trailing whitespace should not affect hash: %q vs %q", hash1, hash2)
	}
}

func TestNormalizeToLF(t *testing.T) {
	if got := NormalizeToLF("a\r\nb\rc\n"); got != "a\nb\nc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStripBOM(t *testing.T) {
	bom, clean := StripBOM("\xEF\xBB\xBFhello")
	if bom != "\xEF\xBB\xBF" {
		t.Fatalf("expected BOM, got %q", bom)
	}
	if clean != "hello" {
		t.Fatalf("expected 'hello', got %q", clean)
	}
	bom, clean = StripBOM("hello")
	if bom != "" {
		t.Fatalf("expected no BOM, got %q", bom)
	}
	if clean != "hello" {
		t.Fatalf("expected 'hello', got %q", clean)
	}
}

// ---------------------------------------------------------------------------
