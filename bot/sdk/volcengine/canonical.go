package volcengine

import (
	"fmt"
	"net/url"
	"strings"
)

const signedHeaders = "content-type;host;x-content-sha256;x-date"

type canonicalRequest struct {
	Method      string
	Path        string
	Query       url.Values
	Host        string
	PayloadHash string
	XDate       string
}

func (r canonicalRequest) String() string {
	canonicalHeaders := strings.Join([]string{
		"content-type:application/json",
		"host:" + r.Host,
		"x-content-sha256:" + r.PayloadHash,
		"x-date:" + r.XDate,
	}, "\n") + "\n"

	canonicalQuery := strings.ReplaceAll(r.Query.Encode(), "+", "%20")
	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		r.Method, r.Path, canonicalQuery, canonicalHeaders, signedHeaders, r.PayloadHash)
}
