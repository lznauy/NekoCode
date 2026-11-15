package secrets

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
)

func TestExistingFiltersRefreshWhenCredentialWasAnEscapedAlias(t *testing.T) {
	original := "escaped-alias-" + rand.Text() + "\nvalue"
	Register(original)
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	alias := string(encoded[1 : len(encoded)-1])
	encoded, err = json.Marshal(alias)
	if err != nil {
		t.Fatal(err)
	}
	input := "before " + string(encoded[1:len(encoded)-1]) + " after"
	filters := make([]*Filter, len(input)+1)
	for i := range filters {
		filters[i] = NewFilter()
	}
	// The alias already exists in the registry, but its escaped spelling does
	// not. Existing streams must learn the newly introduced matching rule.
	Register(alias)
	for split, filter := range filters {
		first := filter.Transform([]byte(input[:split]), false)
		last := filter.Transform([]byte(input[split:]), true)
		if got := string(first) + string(last); got != "before "+Replacement+" after" {
			t.Fatalf("split %d: newly escaped credential leaked: %q", split, got)
		}
	}
}

func TestCredentialsRedactedAcrossEveryChunkBoundary(t *testing.T) {
	key := "credential-sentinel-chunk-test-987654"
	Register(key)
	input := "before " + key + " after"
	for split := 0; split <= len(input); split++ {
		f := NewFilter()
		a := f.Transform([]byte(input[:split]), false)
		if strings.Contains(string(a), key) {
			t.Fatal("stream exposed credential")
		}
		b := f.Transform([]byte(input[split:]), false)
		c := f.Transform(nil, true)
		got := string(a) + string(b) + string(c)
		if got != "before "+Replacement+" after" {
			t.Fatalf("split %d: %q", split, got)
		}
	}
}
func TestNoncredentialAndIncompletePrefixPreserved(t *testing.T) {
	if got := Redact("unrelated ordinary output"); got != "unrelated ordinary output" {
		t.Fatal(got)
	}
	Register("unique-credential-prefix-complete")
	f := NewFilter()
	if got := f.Transform([]byte("unique-credential-prefix-"), false); len(got) != 0 {
		t.Fatalf("partial exposed %q", got)
	}
	if got := string(f.Transform(nil, true)); got != "unique-credential-prefix-" {
		t.Fatal(got)
	}
}

func TestExistingFilterRefreshesNewlyLoadedCredentials(t *testing.T) {
	f := NewFilter()
	key := "rotated-credential-sentinel-456789"
	Register(key)
	first := f.Transform([]byte(key[:17]), false)
	if len(first) != 0 {
		t.Fatalf("partial rotated credential exposed: %q", first)
	}
	last := f.Transform([]byte(key[17:]), true)
	if string(last) != Replacement {
		t.Fatalf("rotated credential not protected: %q", last)
	}
}

func TestRepeatedFilteringIsStableAcrossChunkBoundaries(t *testing.T) {
	Register("CREDENTIAL", "REDACTED", Replacement+"-private-suffix")
	once := Redact("before CREDENTIAL after")
	if twice := Redact(once); twice != once {
		t.Fatalf("repeated filtering changed output: once=%q twice=%q", once, twice)
	}
	for split := 0; split <= len(once); split++ {
		f := NewFilter()
		out := append(f.Transform([]byte(once[:split]), false), f.Transform([]byte(once[split:]), false)...)
		out = append(out, f.Transform(nil, true)...)
		if string(out) != once {
			t.Fatalf("split=%d output=%q", split, out)
		}
	}
	if got := Redact(Replacement + "-private-suffix"); got != Replacement {
		t.Fatalf("credential starting with marker was exposed: %q", got)
	}
}

func TestShortCredentialRemainsProtectedInsideTextAcrossChunks(t *testing.T) {
	credential := "钥匙"
	Register(credential)
	input := "prefix" + credential + "suffix"
	for split := 0; split <= len(input); split++ {
		filter := NewFilter()
		first := filter.Transform([]byte(input[:split]), false)
		last := filter.Transform([]byte(input[split:]), true)
		if string(first)+string(last) != "prefix"+Replacement+"suffix" {
			t.Fatalf("short credential escaped filtering at split %d", split)
		}
	}
}
