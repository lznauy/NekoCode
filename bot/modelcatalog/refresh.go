package modelcatalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nekocode/logger"
	utilfs "nekocode/util/fs"
)

const refreshInterval = 24 * time.Hour

func cachePath() string { return filepath.Join(utilfs.NekocodeDataDir("cache"), "models-dev.json") }

func readCache(path string) (*Catalog, error) {
	data, err := utilfs.ReadRegularFile(context.Background(), path, maxCatalogBytes)
	if err != nil {
		return nil, err
	}
	c, err := parseSnapshot(data)
	if err == nil && c.data.FetchedAt.After(time.Now().Add(5*time.Minute)) {
		return nil, fmt.Errorf("model catalog timestamp is in the future")
	}
	return c, err
}

// Load reads only local data. An invalid or missing cache never blocks startup.
func Load() *Catalog {
	if c, err := readCache(cachePath()); err == nil && c.data.Version >= 3 {
		return c
	}
	return Bundled()
}

// Fetch downloads specifications only, without any model names, API keys,
// configuration or conversation data in the request.
func Fetch(ctx context.Context) (*Catalog, error) {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.Host != "models.dev" {
			return fmt.Errorf("model catalog redirect refused")
		}
		return nil
	}}
	return fetch(ctx, client, SourceURL)
}
func fetch(ctx context.Context, client *http.Client, address string) (*Catalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NekoCode-model-catalog/1")
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model catalog HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogBytes+1))
	if err != nil {
		return nil, err
	}
	return parseAPI(data, time.Now())
}

// RefreshCache updates only disk. Existing configurations retain their snapshot.
func RefreshCache(ctx context.Context) error {
	offline := strings.ToLower(strings.TrimSpace(os.Getenv("NEKOCODE_MODEL_CATALOG_OFFLINE")))
	if offline == "1" || offline == "true" {
		return nil
	}
	return refreshCache(ctx, cachePath(), Fetch)
}
func refreshCache(ctx context.Context, path string, download func(context.Context) (*Catalog, error)) error {
	if cached, err := readCache(path); err == nil && cached.data.Version >= 3 && time.Since(cached.data.FetchedAt) < refreshInterval {
		return nil
	}
	catalog, err := download(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := catalog.MarshalSnapshot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".models-dev-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// RefreshInBackground belongs to executable startup, never config getters or
// constructors. Tests and library callers do not implicitly access the network.
func RefreshInBackground(ctx context.Context) {
	go func() {
		if err := RefreshCache(ctx); err != nil && ctx.Err() == nil {
			logger.Log("model catalog refresh failed; retained local snapshot: %v", err)
		}
	}()
}
