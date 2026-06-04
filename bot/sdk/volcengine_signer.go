package sdk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// VolcSigner generates Volcengine Signature V4 for API requests.
type VolcSigner struct {
	accessKey string
	secretKey string
	region    string
	service   string
}

// NewVolcSigner creates a new Volcengine API signer.
func NewVolcSigner(accessKey, secretKey, region, service string) *VolcSigner {
	return &VolcSigner{accessKey: accessKey, secretKey: secretKey, region: region, service: service}
}

// SignResult holds the headers that must be set on the outgoing request.
type SignResult struct {
	Authorization  string
	XDate          string
	XContentSha256 string
}

// Sign produces a Volcengine Signature V4 for a POST request with JSON body.
// Signed headers: content-type, host, x-content-sha256, x-date.
func (s *VolcSigner) Sign(method, path, host string, query url.Values, body []byte) (*SignResult, error) {
	t := time.Now().UTC()
	xDate := t.Format("20060102T150405Z")
	dateStr := t.Format("20060102")

	payloadHash := sha256Hex(body)

	canonicalHeaders := strings.Join([]string{
		"content-type:application/json",
		"host:" + host,
		"x-content-sha256:" + payloadHash,
		"x-date:" + xDate,
	}, "\n") + "\n"

	signedHeadersStr := "content-type;host;x-content-sha256;x-date"

	canonicalQuery := strings.ReplaceAll(query.Encode(), "+", "%20")

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		method, path, canonicalQuery, canonicalHeaders, signedHeadersStr, payloadHash)

	credentialScope := fmt.Sprintf("%s/%s/%s/request", dateStr, s.region, s.service)
	stringToSign := fmt.Sprintf("HMAC-SHA256\n%s\n%s\n%s",
		xDate, credentialScope, sha256Hex([]byte(canonicalRequest)))

	kSigning := s.deriveSigningKey(dateStr)
	signature := hmacSha256Hex(kSigning, []byte(stringToSign))

	authHeader := fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, credentialScope, signedHeadersStr, signature)

	return &SignResult{
		Authorization:  authHeader,
		XDate:          xDate,
		XContentSha256: payloadHash,
	}, nil
}

func (s *VolcSigner) deriveSigningKey(date string) []byte {
	kDate := hmacSha256Raw([]byte(s.secretKey), []byte(date))
	kRegion := hmacSha256Raw(kDate, []byte(s.region))
	kService := hmacSha256Raw(kRegion, []byte(s.service))
	return hmacSha256Raw(kService, []byte("request"))
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSha256Raw(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func hmacSha256Hex(key, data []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
