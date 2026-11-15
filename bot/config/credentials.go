package config

import (
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"nekocode/util/secrets"
)

// Register the credential fields owned by this schema explicitly; ordinary
// configuration values must not become global output replacement patterns.
// Call only after validation/load or save succeeds. Previously accepted
// credentials stay protected for in-flight requests until the process exits.
func (cfg *Config) registerCredentials() {
	for i, provider := range cfg.Providers {
		registerCredential(fmt.Sprintf("providers[%d].api_key", i), provider.APIKey)
	}
	for i, model := range cfg.ImageGenModels {
		registerCredential(fmt.Sprintf("image_gen_models[%d].api_key", i), model.APIKey)
		registerCredential(fmt.Sprintf("image_gen_models[%d].secret_key", i), model.SecretKey)
	}
	for i, model := range cfg.ImageUnderstandModels {
		registerCredential(fmt.Sprintf("image_understand_models[%d].api_key", i), model.APIKey)
	}
	for name, server := range cfg.MCPServers {
		field := fmt.Sprintf("mcp_servers[%q]", name)
		registerCredential(field+".oauth_client_secret", server.OAuthClientSecret)
		registerNamedCredentials(field+".env", server.Env)
		registerHeaderCredentials(field+".headers", server.Headers)
	}
	if cfg.Jev != nil {
		registerCredential("jev.api_key", cfg.Jev.APIKey)
	}
}

// Short values still require filtering. Warn about output collisions without
// ever including the credential itself or relaxing its protection.
func registerCredential(field, value string) {
	secrets.Register(value)
	if value != "" && utf8.RuneCountInString(value) < 8 {
		slog.Warn("Short credential may cause unrelated tool output to be redacted; redaction remains enabled", "field", field)
	}
}

// HTTP authentication fields carry credentials under protocol-defined names,
// independently of the names used for environment variables.
func registerHeaderCredentials(field string, headers map[string]string) {
	for name, value := range headers {
		path := fmt.Sprintf("%s[%q]", field, name)
		switch {
		case strings.EqualFold(name, "Authorization"):
			// Register the whole header, but assess the token's length rather
			// than counting an authentication scheme such as "Bearer".
			secrets.Register(value)
			if parts := strings.Fields(value); len(parts) == 2 {
				registerCredential(path, parts[1])
			} else {
				registerCredential(path, value)
			}
		case strings.EqualFold(name, "X-API-Key"), isCredentialName(name):
			registerCredential(path, value)
		}
	}
}

// MCP dictionaries have user-selected keys rather than a fixed Go schema.
func registerNamedCredentials(field string, values map[string]string) {
	for key, value := range values {
		if isCredentialName(key) {
			registerCredential(fmt.Sprintf("%s[%q]", field, key), value)
		}
	}
}

func isCredentialName(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	// Preserve word boundaries: vendor prefixes are allowed, trailing metadata
	// such as TOKEN_LIMIT or API_KEY_FILE is not. Compact bare names stay valid.
	for _, name := range []string{"api_key", "token", "password", "secret", "access_token", "refresh_token", "secret_key", "oauth_client_secret"} {
		if key == name || key == strings.ReplaceAll(name, "_", "") || strings.HasSuffix(key, "_"+name) {
			return true
		}
	}
	return false
}
