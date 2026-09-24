package mcp

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

func unionOAuthScopes(groups ...[]string) []string {
	var scopes []string
	for _, group := range groups {
		for _, scope := range group {
			if scope != "" && !slices.Contains(scopes, scope) {
				scopes = append(scopes, scope)
			}
		}
	}
	return scopes
}

// The lock is held by load. Explicit login ignores unusable tokens, but keeps
// previously granted scopes when the credential record is still readable.
func (h *oauthHandler) restoreScopesLocked() {
	path, err := credentialPath(h.name, h.cfg)
	if err != nil {
		return
	}
	if saved, err := readCredential(path); err == nil && saved != nil {
		h.scopes = unionOAuthScopes(h.scopes, saved.Config.Scopes)
	}
}

func (h *oauthHandler) authorizationScopes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.scopes...)
}

func (h *oauthHandler) rememberChallengeScopes(resp *http.Response) {
	challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, challenge := range challenges {
		if challenge.Scheme == "bearer" {
			h.scopes = unionOAuthScopes(h.scopes, strings.Fields(challenge.Params["scope"]))
		}
	}
}

// The SDK reads the first Bearer scope challenge. Prepending the accumulated
// scopes preserves the original resource metadata and error challenges while
// ensuring a fresh handler requests the permissions that prompted login.
func (h *oauthHandler) withAuthorizationScopes(resp *http.Response) *http.Response {
	scopes := h.authorizationScopes()
	if len(scopes) == 0 {
		return resp
	}
	copy := *resp
	copy.Header = resp.Header.Clone()
	key := http.CanonicalHeaderKey("WWW-Authenticate")
	copy.Header[key] = append([]string{"Bearer scope=" + strconv.Quote(strings.Join(scopes, " "))}, copy.Header[key]...)
	return &copy
}
