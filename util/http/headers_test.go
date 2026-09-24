package http

import "testing"

func TestNormalizeHeaders(t *testing.T) {
	normalized, err := NormalizeHeaders(map[string]string{
		"authorization": " Bearer token ",
		"X-Custom":      "value",
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized["Authorization"] != "Bearer token" {
		t.Fatalf("Authorization = %q", normalized["Authorization"])
	}
	if normalized["X-Custom"] != "value" {
		t.Fatalf("X-Custom = %q", normalized["X-Custom"])
	}
}

func TestNormalizeHeadersRejects(t *testing.T) {
	cases := map[string]map[string]string{
		"empty name":     {"": "value"},
		"invalid name":   {"bad name": "value"},
		"newline in key": {"X\r-Evil": "value"},
		"hop-by-hop":     {"Host": "evil.example.com"},
		"CRLF in value":  {"Authorization": "Bearer a\r\nX-Evil: yes"},
		"NUL in value":   {"Authorization": "a\x00"},
	}
	for name, headers := range cases {
		if _, err := NormalizeHeaders(headers); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestNormalizeHeadersEmpty(t *testing.T) {
	normalized, err := NormalizeHeaders(nil)
	if err != nil || normalized != nil {
		t.Fatalf("nil input: %v %v", normalized, err)
	}
}
