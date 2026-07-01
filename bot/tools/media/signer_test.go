package media

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignerUsesStableDateAndPayloadHash(t *testing.T) {
	signer := NewSignerWithClock("ak", "sk", "cn-north-1", "cv", func() time.Time {
		return time.Date(2026, 6, 19, 12, 34, 56, 0, time.UTC)
	})
	query := url.Values{"q": {"a b"}}
	got, err := signer.Sign("POST", "/api", "example.com", query, []byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.XDate != "20260619T123456Z" {
		t.Fatalf("XDate = %q", got.XDate)
	}
	if got.XContentSha256 != sha256Hex([]byte(`{"x":1}`)) {
		t.Fatalf("bad payload hash")
	}
	if !strings.Contains(got.Authorization, "Credential=ak/20260619/cn-north-1/cv/request") {
		t.Fatalf("bad authorization scope: %q", got.Authorization)
	}
	if !strings.Contains(got.Authorization, "SignedHeaders="+signedHeaders) {
		t.Fatalf("bad signed headers: %q", got.Authorization)
	}
}

func TestCanonicalQueryEscapesSpacesAsPercent20(t *testing.T) {
	q := url.Values{"q": {"a b"}}
	req := canonicalRequest{
		Method:      "POST",
		Path:        "/api",
		Query:       q,
		Host:        "example.com",
		PayloadHash: "hash",
		XDate:       "20260619T123456Z",
	}
	if !strings.Contains(req.String(), "\nq=a%20b\n") {
		t.Fatalf("canonical query did not escape space as %%20: %q", req.String())
	}
}
