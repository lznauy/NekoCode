package sdk

import (
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
