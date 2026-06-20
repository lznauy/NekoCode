package volcengine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func deriveSigningKey(secretKey, date, region, service string) []byte {
	kDate := hmacSha256Raw([]byte(secretKey), []byte(date))
	kRegion := hmacSha256Raw(kDate, []byte(region))
	kService := hmacSha256Raw(kRegion, []byte(service))
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
