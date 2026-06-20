package sdk

import (
	"net/url"

	"nekocode/bot/sdk/volcengine"
)

// VolcSigner generates Volcengine Signature V4 for API requests.
type VolcSigner = volcengine.Signer

// NewVolcSigner creates a new Volcengine API signer.
func NewVolcSigner(accessKey, secretKey, region, service string) *VolcSigner {
	return volcengine.NewSigner(accessKey, secretKey, region, service)
}

// SignResult holds the headers that must be set on the outgoing request.
type SignResult = volcengine.SignResult

// Sign produces a Volcengine Signature V4 for a POST request with JSON body.
// Signed headers: content-type, host, x-content-sha256, x-date.
func SignVolcengine(s *VolcSigner, method, path, host string, query url.Values, body []byte) (*SignResult, error) {
	return s.Sign(method, path, host, query, body)
}
