package media

import (
	"fmt"
	"net/url"
	"time"
)

// Signer generates Volcengine Signature V4 for API requests.
type Signer struct {
	accessKey string
	secretKey string
	region    string
	service   string
	now       func() time.Time
}

func NewSigner(accessKey, secretKey, region, service string) *Signer {
	return &Signer{
		accessKey: accessKey,
		secretKey: secretKey,
		region:    region,
		service:   service,
		now:       time.Now,
	}
}

func NewSignerWithClock(accessKey, secretKey, region, service string, now func() time.Time) *Signer {
	s := NewSigner(accessKey, secretKey, region, service)
	s.now = now
	return s
}

// SignResult holds the headers that must be set on the outgoing request.
type SignResult struct {
	Authorization  string
	XDate          string
	XContentSha256 string
}

// Sign produces a Volcengine Signature V4 for a POST request with JSON body.
func (s *Signer) Sign(method, path, host string, query url.Values, body []byte) (*SignResult, error) {
	now := s.now
	if now == nil {
		now = time.Now
	}
	t := now().UTC()
	xDate := t.Format("20060102T150405Z")
	dateStr := t.Format("20060102")
	payloadHash := sha256Hex(body)

	canonical := canonicalRequest{
		Method:      method,
		Path:        path,
		Query:       query,
		Host:        host,
		PayloadHash: payloadHash,
		XDate:       xDate,
	}
	canonicalRequestText := canonical.String()
	credentialScope := fmt.Sprintf("%s/%s/%s/request", dateStr, s.region, s.service)
	stringToSign := fmt.Sprintf("HMAC-SHA256\n%s\n%s\n%s",
		xDate, credentialScope, sha256Hex([]byte(canonicalRequestText)))

	kSigning := s.deriveSigningKey(dateStr)
	signature := hmacSha256Hex(kSigning, []byte(stringToSign))

	return &SignResult{
		Authorization: fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
			s.accessKey, credentialScope, signedHeaders, signature),
		XDate:          xDate,
		XContentSha256: payloadHash,
	}, nil
}

func (s *Signer) deriveSigningKey(date string) []byte {
	return deriveSigningKey(s.secretKey, date, s.region, s.service)
}
