// Package project reads optional configuration belonging to one project root.
// It does not change the process directory or grant filesystem permissions.
package project

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	utilhttp "nekocode/util/http"
)

const MaxInstructionsBytes = 128 * 1024
const maxMCPBytes = 1024 * 1024

// Server is a complete project override, never a field-wise merge.
type Server struct {
	URL                    string            `json:"url,omitempty"`
	Headers                map[string]string `json:"headers,omitempty"`
	OAuthClientID          string            `json:"oauth_client_id,omitempty"`
	OAuthClientSecret      string            `json:"oauth_client_secret,omitempty"`
	OAuthClientMetadataURL string            `json:"oauth_client_metadata_url,omitempty"`
	OAuthCallbackPort      int               `json:"oauth_callback_port,omitempty"`

	Command string
	Args    []string
	Env     map[string]string
	CWD     string
	Enabled bool
}

// Project holds the last valid contents of each independently optional file.
// The owner serializes Refresh with agent runs; Root remains fixed.
type Project struct {
	Root         string
	Instructions string
	Servers      map[string]Server
	Diagnostics  []string
}

func New(root string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project root %q is not a directory", abs)
	}
	return &Project{Root: abs}, nil
}

func (p *Project) InstructionsPath() string { return filepath.Join(p.Root, "NEKOCODE.md") }
func (p *Project) MCPPath() string          { return filepath.Join(p.Root, ".nekocode", ".mcp.json") }

// Refresh retains the last valid value on read/parse failure. Missing files
// explicitly clear their value. Failures in one file do not block the other.
func (p *Project) Refresh() []error {
	var diagnostics []error
	data, err := readOptional(p.InstructionsPath(), MaxInstructionsBytes)
	if err == nil && !utf8.Valid(data) {
		err = fmt.Errorf("%s: expected UTF-8 text", p.InstructionsPath())
	}
	if err != nil {
		diagnostics = append(diagnostics, err)
	} else {
		p.Instructions = strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	}
	servers, err := readServers(p.MCPPath(), p.Root)
	if err != nil {
		diagnostics = append(diagnostics, err)
	} else {
		p.Servers = servers
	}
	p.Diagnostics = nil
	for _, err := range diagnostics {
		p.Diagnostics = append(p.Diagnostics, err.Error())
	}
	return diagnostics
}

func readOptional(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: expected a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: exceeds %d bytes", path, limit)
	}
	return data, nil
}

func readServers(path, root string) (map[string]Server, error) {
	data, err := readOptional(path, maxMCPBytes)
	if err != nil || data == nil {
		return nil, err
	}
	var document struct {
		Servers map[string]struct {
			URL                    string            `json:"url,omitempty"`
			Headers                map[string]string `json:"headers,omitempty"`
			OAuthClientID          string            `json:"oauth_client_id,omitempty"`
			OAuthClientSecret      string            `json:"oauth_client_secret,omitempty"`
			OAuthClientMetadataURL string            `json:"oauth_client_metadata_url,omitempty"`
			OAuthCallbackPort      int               `json:"oauth_callback_port,omitempty"`

			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			CWD     string            `json:"cwd"`
			Enabled *bool             `json:"enabled"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if document.Servers == nil {
		return nil, fmt.Errorf("%s: expected an mcpServers object (use {\"mcpServers\": {}} for no servers)", path)
	}
	// Validate in a stable order so diagnostics do not depend on map iteration.
	names := make([]string, 0, len(document.Servers))
	for name := range document.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make(map[string]Server, len(document.Servers))
	for _, name := range names {
		raw := document.Servers[name]
		if name == "" || strings.TrimSpace(name) != name {
			return nil, fmt.Errorf("%s: invalid server name %q", path, name)
		}
		enabled := raw.Enabled == nil || *raw.Enabled
		raw.Command = strings.TrimSpace(raw.Command)
		var err error
		raw.URL, err = utilhttp.NormalizeSecureURL(raw.URL)
		if err != nil {
			return nil, fmt.Errorf("%s: server %q URL: %w", path, name, err)
		}
		raw.OAuthClientMetadataURL, err = utilhttp.NormalizeSecureURL(raw.OAuthClientMetadataURL)
		if err != nil {
			return nil, fmt.Errorf("%s: server %q OAuth metadata URL: %w", path, name, err)
		}
		raw.OAuthClientID = strings.TrimSpace(raw.OAuthClientID)
		raw.OAuthClientSecret = strings.TrimSpace(raw.OAuthClientSecret)
		headers, err := utilhttp.NormalizeHeaders(raw.Headers)
		if err != nil {
			return nil, fmt.Errorf("%s: server %q: %w", path, name, err)
		}
		raw.Headers = headers
		if raw.URL != "" {
			if raw.Command != "" {
				return nil, fmt.Errorf("MCP server must specify command or URL, not both")
			}
		}
		if raw.OAuthCallbackPort < 0 || raw.OAuthCallbackPort > 65535 {
			return nil, fmt.Errorf("invalid OAuth callback port")
		}

		if enabled && strings.TrimSpace(raw.Command) == "" && raw.URL == "" {
			return nil, fmt.Errorf("%s: server %q requires command", path, name)
		}
		for _, value := range append([]string{raw.Command, raw.CWD}, raw.Args...) {
			if strings.ContainsRune(value, '\x00') {
				return nil, fmt.Errorf("%s: server %q has NUL in launch parameters", path, name)
			}
		}
		for key, value := range raw.Env {
			if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
				return nil, fmt.Errorf("%s: server %q has invalid environment entry", path, name)
			}
		}
		cwd := raw.CWD
		if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(root, cwd)
		}
		command := raw.Command
		// A relative executable path is relative to the server's cwd; bare
		// names continue to use PATH. Arguments are never rewritten.
		if !filepath.IsAbs(command) && strings.ContainsAny(command, `/\`) {
			command = filepath.Join(cwd, command)
		}
		servers[name] = Server{URL: raw.URL, Headers: raw.Headers, OAuthClientID: raw.OAuthClientID, OAuthClientSecret: raw.OAuthClientSecret, OAuthClientMetadataURL: raw.OAuthClientMetadataURL, OAuthCallbackPort: raw.OAuthCallbackPort, Command: command, Args: raw.Args, Env: raw.Env,
			CWD: filepath.Clean(cwd), Enabled: enabled}
	}
	return servers, nil
}
