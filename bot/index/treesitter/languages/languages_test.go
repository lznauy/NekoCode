package languages

import "testing"

func TestLanguagesIncludeSupportedExtensions(t *testing.T) {
	for _, ext := range []string{".go", ".js", ".jsx", ".mjs", ".ts", ".tsx", ".py", ".rs"} {
		if Languages[ext] == nil {
			t.Fatalf("missing language for %s", ext)
		}
	}
}
