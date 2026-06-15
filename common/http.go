// http.go — 全局共享的 HTTP Transport，统一连接池配置。
package common

import (
	"net/http"
	"time"
)

// SharedTransport is the global shared HTTP transport for all HTTP clients
// (LLM requests, tool HTTP requests, etc.). Sharing a single transport
// enables connection pooling across packages.
var SharedTransport = &http.Transport{
	MaxIdleConns:        20,
	MaxIdleConnsPerHost: 10,
	IdleConnTimeout:     90 * time.Second,
}
