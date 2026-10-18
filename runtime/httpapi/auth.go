package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func WithBearerAuth(next http.Handler, token string) http.Handler {
	return WithOptions(next, Options{Token: token})
}

// Options configures the HTTP surface for external applications.
type Options struct {
	// Token enables bearer authentication. /health stays public.
	Token string
	// AllowOrigin enables CORS for read-only GETs. When non-empty, the
	// preflight is answered for the listed origin and observation responses
	// carry Access-Control-Allow-Origin. Credentials are never allowed, so an
	// origin cannot read authenticated data without its own token.
	AllowOrigin string
}

// WithOptions wraps the handler with authentication and optional CORS.
// CORS sits outside auth so an OPTIONS preflight — which never carries the
// token — is answered without touching the authenticated surface.
func WithOptions(next http.Handler, opts Options) http.Handler {
	token := strings.TrimSpace(opts.Token)
	origin := strings.TrimSpace(opts.AllowOrigin)
	handler := next
	if token != "" {
		handler = bearerAuth(next, token)
	}
	if origin != "" {
		handler = withCORS(handler, origin)
	}
	return handler
}

func bearerAuth(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		// The browser cannot set headers on an SSE EventSource or on a plain
		// navigation, so an observation-only client may instead pass the token
		// as a query parameter. CORS-ready reads are the intended use.
		if authorized(r, token) || authorizedQuery(r, token) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="nekocode"`)
		writeError(w, http.StatusUnauthorized, "unauthorized")
	})
}

// withCORS answers preflight requests and annotates responses. Credentials are
// deliberately not enabled: a token must still be supplied explicitly.
func withCORS(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestOrigin := strings.TrimSpace(r.Header.Get("Origin"))
		if requestOrigin == "" || !originAllowed(origin, requestOrigin) {
			if r.Method == http.MethodOptions {
				writeError(w, http.StatusForbidden, "origin not allowed")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", requestOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-Nekocode-Token, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Set("Vary", "Origin")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// CORS covers read-only GETs only. Annotating a mutating response
		// would let any allowed origin read what it changed, contradicting
		// the observation-only contract (and the preflight advertises GET).
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", requestOrigin)
		w.Header().Set("Vary", "Origin")
		next.ServeHTTP(w, r)
	})
}

// originAllowed accepts "*" or an exact origin from a comma-separated list.
func originAllowed(allow, request string) bool {
	for _, candidate := range strings.Split(allow, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.EqualFold(candidate, request) {
			return true
		}
	}
	return false
}

func authorized(r *http.Request, token string) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		candidate := strings.TrimSpace(auth[len("bearer "):])
		return subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1
	}
	candidate := strings.TrimSpace(r.Header.Get("X-Nekocode-Token"))
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1
}

// authorizedQuery accepts the token as ?token= for clients that cannot set
// headers (EventSource, iframe, plain navigation). It is limited to GET: a URL
// parameter ends up in access logs and browser history, so it must never be
// able to authorize a state-changing request.
func authorizedQuery(r *http.Request, token string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	candidate := strings.TrimSpace(r.URL.Query().Get("token"))
	if candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1
}
