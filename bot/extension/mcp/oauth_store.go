package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	utilhttp "nekocode/util/http"

	"golang.org/x/oauth2"
)

// Tokens never enter project config, runtime views or model messages. The
// per-user store and files are private; writes atomically replace credentials.
type credentialRecord struct {
	Generation string        `json:"generation,omitempty"`
	Config     oauth2.Config `json:"config"`
	Token      oauth2.Token  `json:"token"`
}

func credentialPath(name string, cfg ServerConfig) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(name + "\x00" + cfg.CWD + "\x00" + cfg.URL + "\x00" + cfg.OAuthClientID + "\x00" + cfg.OAuthClientSecret + "\x00" + cfg.OAuthClientMetadataURL))
	return filepath.Join(home, ".nekocode", "credentials", "mcp", hex.EncodeToString(key[:])+".json"), nil
}
func readCredential(path string) (*credentialRecord, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read MCP credentials: %w", err)
	}
	var record credentialRecord
	if json.Unmarshal(data, &record) != nil {
		return nil, fmt.Errorf("invalid MCP credential file")
	}
	if err := utilhttp.ValidateSecureURL(record.Config.Endpoint.TokenURL); err != nil {
		return nil, err
	}
	return &record, nil
}
func writeCredential(path string, record credentialRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return withCredentialLock(ctx, path, func() error { return writeCredentialLocked(path, record) })
}
func writeCredentialLocked(path string, record credentialRecord) error {
	if record.Generation == "" {
		record.Generation = rand.Text()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".oauth-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(record); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	// The refresh token has no other copy: fsync the directory so the rename
	// itself survives a power loss, not just the file contents.
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

type persistentTokenSource struct {
	ctx        context.Context
	generation string
	mu         sync.Mutex
	source     oauth2.TokenSource
	cfg        oauth2.Config
	token      oauth2.Token
	path       string
}

func (s *persistentTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var token *oauth2.Token
	err := withCredentialLock(ctx, s.path, func() error {
		// Always re-read, including for cached access tokens: another process may
		// have rotated them, logged out, or signed into a different account.
		saved, err := readCredential(s.path)
		if err != nil {
			return err
		}
		if saved == nil || saved.Generation != s.generation {
			return errAuthorizationRequired
		}
		if !reflect.DeepEqual(saved.Token, s.token) || !reflect.DeepEqual(saved.Config, s.cfg) {
			s.cfg, s.token = saved.Config, saved.Token
			s.source = s.cfg.TokenSource(ctx, &s.token)
		}
		if !s.token.Valid() && s.token.RefreshToken == "" {
			if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
				return err
			}
			return errAuthorizationRequired
		}
		next, err := s.source.Token()
		if err != nil {
			// Generation was checked under this same lock: only this login's
			// rejected grant may be removed, never a newer login from a peer.
			var retrieve *oauth2.RetrieveError
			if errors.As(err, &retrieve) && (retrieve.ErrorCode == "invalid_grant" || retrieve.ErrorCode == "invalid_client") {
				if removeErr := os.Remove(s.path); removeErr != nil && !os.IsNotExist(removeErr) {
					return removeErr
				}
				return errAuthorizationRequired
			}
			return err
		}
		if next.AccessToken != s.token.AccessToken || next.RefreshToken != s.token.RefreshToken || !next.Expiry.Equal(s.token.Expiry) {
			if err := writeCredentialLocked(s.path, credentialRecord{Config: s.cfg, Token: *next, Generation: s.generation}); err != nil {
				return fmt.Errorf("save MCP credentials: %w", err)
			}
			s.token = *next
		}
		copy := *next
		token = &copy
		return nil
	})
	return token, err
}
