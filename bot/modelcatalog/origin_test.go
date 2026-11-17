package modelcatalog

import (
	"reflect"
	"testing"
)

func TestOriginRequiresAuthorOwnedExactModelAndReturnsNoLimits(t *testing.T) {
	raw := `{"author":{"api":"https://official.example/v1","models":{"exact":{"id":"exact","canonical_model_id":"author/exact","modalities":{"output":["text"]},"limit":{"context":96000},"reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high"]}]}}},"reseller":{"api":"https://reseller.example/v1","models":{"only-reseller":{"id":"only-reseller","canonical_model_id":"author/only-reseller","modalities":{"output":["text"]},"limit":{"context":96000},"reasoning":true,"reasoning_options":[{"type":"effort","values":["max"]}]}}}}`
	c, err := Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	origin, ok := c.OriginReasoning("exact")
	if !ok || origin.Provider != "author" || !reflect.DeepEqual(origin.Reasoning.Efforts, []string{"low", "high"}) {
		t.Fatalf("origin lost: %+v", origin)
	}
	for _, id := range []string{"exact-free", "EXACT", "prefix/exact", "only-reseller"} {
		if _, ok := c.OriginReasoning(id); ok {
			t.Fatalf("non-author or approximate ID matched: %s", id)
		}
	}
	origin.Reasoning.Efforts[0] = "max"
	again, _ := c.OriginReasoning("exact")
	if again.Reasoning.Efforts[0] != "low" {
		t.Fatal("origin mutated")
	}
	if _, ok := c.Lookup("https://unlisted-relay.example/v1", "exact"); ok {
		t.Fatal("reasoning fallback supplied service limits")
	}
	rawSnapshot, err := c.MarshalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := parseSnapshot(rawSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.OriginReasoning("exact"); !ok {
		t.Fatal("origin lost in cache")
	}
}
func TestConflictingAuthorsDoNotCreateFallback(t *testing.T) {
	raw := `{"a":{"api":"https://a.example","models":{"same":{"id":"same","canonical_model_id":"a/same","modalities":{"output":["text"]},"limit":{"context":96000},"reasoning":true,"reasoning_options":[{"type":"effort","values":["low"]}]}}},"b":{"api":"https://b.example","models":{"same":{"id":"same","canonical_model_id":"b/same","modalities":{"output":["text"]},"limit":{"context":96000},"reasoning":true,"reasoning_options":[{"type":"effort","values":["high"]}]}}}}`
	c, err := Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.OriginReasoning("same"); ok {
		t.Fatal("ambiguous model author was guessed")
	}
}
