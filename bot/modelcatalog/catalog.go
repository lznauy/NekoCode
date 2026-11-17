// Package modelcatalog provides immutable public model limits. It never changes
// provider routing, credentials, reasoning protocols, or media permissions.
package modelcatalog

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"
)

const SourceURL = "https://models.dev/api.json"
const maxCatalogBytes = 16 << 20
const maxTokens = 128 << 20

//go:generate go run ../../scripts/update-model-catalog -output snapshot.json
//go:embed snapshot.json
var bundled []byte

// Limits are token counts; zero input/output means unspecified.
type Limits struct {
	Context   int        `json:"context"`
	Input     int        `json:"input,omitempty"`
	Output    int        `json:"output,omitempty"`
	Reasoning *Reasoning `json:"reasoning,omitempty"`
}
type snapshot struct {
	Version   int                          `json:"version"`
	FetchedAt time.Time                    `json:"fetched_at"`
	Endpoints map[string]map[string]Limits `json:"endpoints"`
	Origins   map[string]Origin            `json:"origins,omitempty"`
}

// Catalog is immutable after parsing, so configurations can share one snapshot.
type Catalog struct{ data snapshot }

var embedded = sync.OnceValue(func() *Catalog {
	catalog, err := parseSnapshot(bundled)
	if err != nil {
		panic("invalid bundled model catalog: " + err.Error())
	}
	return catalog
})

func Bundled() *Catalog { return embedded() }

func (c *Catalog) Lookup(endpoint, model string) (Limits, bool) {
	if c == nil {
		c = Bundled()
	}
	endpoint = normalizeEndpoint(endpoint)
	if endpoint == "" {
		return Limits{}, false
	}
	if limits, ok := c.data.Endpoints[endpoint][model]; ok {
		return cloneLimits(limits), true
	}
	// Prefer an exact API path. Only then try the common service-root /v1
	// spelling; never strip arbitrary path components or model prefixes.
	alias := endpoint + "/v1"
	if strings.HasSuffix(endpoint, "/v1") {
		alias = strings.TrimSuffix(endpoint, "/v1")
		if strings.HasSuffix(alias, "/v1") {
			return Limits{}, false
		}
	}
	limits, ok := c.data.Endpoints[alias][model]
	return cloneLimits(limits), ok
}

func normalizeEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	// Catalogs often list the service root while compatible clients append /v1.
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/")
}

func validLimits(l Limits) bool {
	return validReasoning(l.Reasoning) && l.Context > 0 && l.Context <= maxTokens && l.Input >= 0 && l.Input <= l.Context && l.Output >= 0 && l.Output <= l.Context
}

func parseSnapshot(data []byte) (*Catalog, error) {
	if len(data) > maxCatalogBytes {
		return nil, fmt.Errorf("model catalog exceeds size limit")
	}
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if (s.Version < 1 || s.Version > 3) || s.FetchedAt.IsZero() || len(s.Endpoints) == 0 {
		return nil, fmt.Errorf("invalid model catalog snapshot")
	}
	count := 0
	for endpoint, models := range s.Endpoints {
		if normalizeEndpoint(endpoint) == "" || normalizeEndpoint(endpoint) != endpoint {
			return nil, fmt.Errorf("invalid model catalog endpoint %q", endpoint)
		}
		for id, limits := range models {
			if strings.TrimSpace(id) == "" || !validLimits(limits) {
				return nil, fmt.Errorf("invalid model limits for %q", id)
			}
			count++
		}
	}
	for id, origin := range s.Origins {
		if strings.TrimSpace(id) == "" || origin.Provider == "" || origin.ModelID != id || origin.Reasoning == nil || !validReasoning(origin.Reasoning) {
			return nil, fmt.Errorf("invalid origin reasoning for %q", id)
		}
	}
	if len(s.Origins) > 50000 {
		return nil, fmt.Errorf("too many origin records")
	}
	if count == 0 || count > 50000 {
		return nil, fmt.Errorf("invalid model catalog entry count")
	}
	return &Catalog{data: s}, nil
}

// MarshalSnapshot emits the compact, deterministic subset used offline.
func (c *Catalog) MarshalSnapshot() ([]byte, error) { return json.MarshalIndent(c.data, "", "  ") }

// Decode imports a downloaded provider catalog without accessing the network.
func Decode(data []byte) (*Catalog, error) { return parseAPI(data, time.Now()) }

func parseAPI(data []byte, now time.Time) (*Catalog, error) {
	if len(data) > maxCatalogBytes {
		return nil, fmt.Errorf("models.dev response exceeds size limit")
	}
	type apiModel struct {
		ID               string          `json:"id"`
		CanonicalID      string          `json:"canonical_model_id"`
		Reasoning        *bool           `json:"reasoning"`
		ReasoningOptions json.RawMessage `json:"reasoning_options"`
		Limit            Limits          `json:"limit"`
		Modalities       struct {
			Output []string `json:"output"`
		} `json:"modalities"`
		Provider struct {
			API string `json:"api"`
		} `json:"provider"`
	}
	var providers map[string]struct {
		API    string              `json:"api"`
		Models map[string]apiModel `json:"models"`
	}
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, err
	}
	endpoints := map[string]map[string]Limits{}
	ambiguous := map[string]bool{}
	origins := map[string]Origin{}
	ambiguousOrigins := map[string]bool{}
	for provider, p := range providers {
		api := p.API
		// These official providers omit their well-known default API in the feed.
		if api == "" {
			switch provider {
			case "openai":
				api = "https://api.openai.com/v1"
			case "anthropic":
				api = "https://api.anthropic.com/v1"
			}
		}
		for id, m := range p.Models {
			endpoint := api
			if m.Provider.API != "" {
				endpoint = m.Provider.API
			}
			endpoint = normalizeEndpoint(endpoint)
			textOutput := false
			for _, modality := range m.Modalities.Output {
				if modality == "text" {
					textOutput = true
				}
			}
			if endpoint == "" || id != m.ID || !textOutput || !validLimits(m.Limit) {
				continue
			}
			m.Limit.Reasoning = parseReasoning(m.Reasoning, m.ReasoningOptions)
			owner, _, canonical := strings.Cut(m.CanonicalID, "/")
			// Use author-owned provider records only, not third-party resellers with
			// the same ID. The feed itself declares the canonical model's author.
			if canonical && owner == provider && m.Limit.Reasoning != nil && !ambiguousOrigins[id] {
				origin := Origin{Provider: provider, ModelID: id, Reasoning: m.Limit.Reasoning}
				if old, exists := origins[id]; exists && !reflect.DeepEqual(old, origin) {
					delete(origins, id)
					ambiguousOrigins[id] = true
				} else {
					origins[id] = origin
				}
			}

			key := endpoint + "\x00" + id
			if ambiguous[key] {
				continue
			}
			if endpoints[endpoint] == nil {
				endpoints[endpoint] = map[string]Limits{}
			}
			if old, exists := endpoints[endpoint][id]; exists && !reflect.DeepEqual(old, m.Limit) {
				delete(endpoints[endpoint], id)
				ambiguous[key] = true
				continue
			}
			endpoints[endpoint][id] = m.Limit
		}
	}
	s := snapshot{Version: 3, FetchedAt: now.UTC(), Endpoints: endpoints, Origins: origins}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return parseSnapshot(bytes.TrimSpace(raw))
}
