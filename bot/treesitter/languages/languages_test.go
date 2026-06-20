package languages

import "testing"

func TestLanguagesIncludeSupportedExtensions(t *testing.T) {
	for _, ext := range []string{".go", ".js", ".jsx", ".mjs", ".ts", ".tsx", ".py", ".rs"} {
		if Languages[ext] == nil {
			t.Fatalf("missing language for %s", ext)
		}
	}
}

func TestNewParsersCreatesParserForEveryLanguage(t *testing.T) {
	parsers := NewParsers()
	if len(parsers) != len(Languages) {
		t.Fatalf("got %d parsers, want %d", len(parsers), len(Languages))
	}
	for ext := range Languages {
		if parsers[ext] == nil {
			t.Fatalf("missing parser for %s", ext)
		}
	}
}
