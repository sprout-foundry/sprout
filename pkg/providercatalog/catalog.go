package providercatalog

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed providers.json
var embeddedCatalogJSON []byte

const defaultCatalogURL = "https://raw.githubusercontent.com/alantheprice/ledit/main/pkg/providercatalog/providers.json"
const maxCatalogBytes int64 = 1 << 20

type Catalog struct {
	UpdatedAt string     `json:"updated_at"`
	Source    string     `json:"source"`
	Providers []Provider `json:"providers"`
}

type Provider struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Description         string  `json:"description,omitempty"`
	SetupHint           string  `json:"setup_hint,omitempty"`
	DocsURL             string  `json:"docs_url,omitempty"`
	SignupURL           string  `json:"signup_url,omitempty"`
	APIKeyLabel         string  `json:"api_key_label,omitempty"`
	APIKeyHelp          string  `json:"api_key_help,omitempty"`
	Recommended         bool    `json:"recommended,omitempty"`
	DefaultModel        string  `json:"default_model,omitempty"`
	RecommendedModel    string  `json:"recommended_model,omitempty"`
	RecommendedModelWhy string  `json:"recommended_model_why,omitempty"`
	Models              []Model `json:"models,omitempty"`
}

type Model struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextLength int      `json:"context_length,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	InputCost     float64  `json:"input_cost,omitempty"`
	OutputCost    float64  `json:"output_cost,omitempty"`
}

var (
	mu            sync.RWMutex
	current       Catalog
	loadOnce      sync.Once
	lastRefreshAt time.Time
)

func ensureLoaded() {
	loadOnce.Do(func() {
		current = mustParseCatalog(embeddedCatalogJSON)
	})
}

func mustParseCatalog(data []byte) Catalog {
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		panic(fmt.Sprintf("providercatalog: failed to parse embedded catalog: %v", err))
	}
	return catalog
}

func CatalogURL() string {
	if override := strings.TrimSpace(os.Getenv("LEDIT_PROVIDER_CATALOG_URL")); override != "" {
		return override
	}
	return defaultCatalogURL
}

func Current() Catalog {
	ensureLoaded()
	mu.RLock()
	defer mu.RUnlock()
	return cloneCatalog(current)
}

func SetCatalog(catalog Catalog) {
	ensureLoaded()
	mu.Lock()
	defer mu.Unlock()
	current = cloneCatalog(catalog)
	lastRefreshAt = time.Now()
}

func FindProvider(id string) (Provider, bool) {
	catalog := Current()
	for _, provider := range catalog.Providers {
		if strings.EqualFold(strings.TrimSpace(provider.ID), strings.TrimSpace(id)) {
			return provider, true
		}
	}
	return Provider{}, false
}

func RefreshFromRemote(ctx context.Context, url string) error {
	ensureLoaded()
	if strings.TrimSpace(url) == "" {
		url = CatalogURL()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ledit-provider-catalog/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("catalog fetch failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
	if err != nil {
		return fmt.Errorf("read catalog response body: %w", err)
	}

	var catalog Catalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return fmt.Errorf("unmarshal catalog: %w", err)
	}

	SetCatalog(catalog)
	return nil
}

func RefreshFromRemoteAsync(url string) {
	ensureLoaded()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = RefreshFromRemote(ctx, url)
	}()
}

func cloneCatalog(catalog Catalog) Catalog {
	out := catalog
	out.Providers = make([]Provider, len(catalog.Providers))
	for i, provider := range catalog.Providers {
		outProvider := provider
		outProvider.Models = append([]Model(nil), provider.Models...)
		for j := range outProvider.Models {
			outProvider.Models[j].Tags = append([]string(nil), provider.Models[j].Tags...)
		}
		out.Providers[i] = outProvider
	}
	return out
}
