package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	controlruntime "nekocode/runtime"
	"nekocode/runtime/a2aapi"
	"nekocode/runtime/httpapi"
)

// newDaemonHandler keeps the existing HTTP/CORS surface unchanged unless A2A
// is explicitly enabled. Network-listener restrictions apply only to A2A.
func newDaemonHandler(rt *controlruntime.Runtime, addr string, options httpapi.Options, enableA2A bool, publicURL string) (http.Handler, error) {
	api := httpapi.WithOptions(httpapi.New(rt).Handler(), options)
	if !enableA2A {
		return api, nil
	}
	if err := validateListenSecurity(addr, options.Token); err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicURL) == "" {
		publicURL = strings.TrimRight(accessURLs(addr)[0], "/") + "/a2a"
	}
	server, err := a2aapi.New(rt, a2aapi.Options{Endpoint: publicURL, Secured: strings.TrimSpace(options.Token) != ""})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /.well-known/agent-card.json", server.AgentCardHandler())
	mux.Handle("/a2a/", httpapi.WithOptions(http.StripPrefix("/a2a", server.Handler()), options))
	mux.Handle("/", api)
	return mux, nil
}

func validateListenSecurity(addr, token string) error {
	if strings.TrimSpace(token) != "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("refusing unauthenticated A2A listener %q: invalid address: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing unauthenticated A2A network listener %q: set -token or NEKOCODE_DAEMON_TOKEN", addr)
}
