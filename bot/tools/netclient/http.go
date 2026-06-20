package netclient

import (
	"net/http"
	"time"

	"nekocode/common"
)

func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: common.SharedTransport,
		Timeout:   timeout,
	}
}
