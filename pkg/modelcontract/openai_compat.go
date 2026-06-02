package modelcontract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// OpenAICompatAdapter is a generic adapter for any provider that exposes a
// standard OpenAI-compatible /v1/models endpoint. These providers return
// { "data": [{ "id": "..." }] } and require a Bearer API key for auth.
type OpenAICompatAdapter struct {
	ProviderID string // provider identifier (e.g. "cerebras")
	BaseURL    string // full URL to /v1/models (e.g. "https://api.cerebras.ai/v1/models")
	EnvVar     string // environment variable name for the API key (e.g. "CEREBRAS_API_KEY")
	HTTPClient *http.Client
}

// NewOpenAICompatAdapter creates a new adapter for an OpenAI-compatible
// provider. The ProviderID is used for credential resolution and the
// Provider() return value.
func NewOpenAICompatAdapter(providerID, baseURL, envVar string) OpenAICompatAdapter {
	return OpenAICompatAdapter{
		ProviderID: strings.ToLower(strings.TrimSpace(providerID)),
		BaseURL:    strings.TrimSpace(baseURL),
		EnvVar:     strings.TrimSpace(envVar),
	}
}

func (a OpenAICompatAdapter) Provider() string { return a.ProviderID }

func (a OpenAICompatAdapter) ListModels(ctx context.Context) ([]CanonicalModel, error) {
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", a.ProviderID, err)
	}
	req.Header.Set("Accept", "application/json")

	// Resolve API key for this provider
	apiKey, _ := credentials.ResolveProviderAPIKey(a.ProviderID, a.EnvVar)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: fetch models: %w", a.ProviderID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: fetch models: HTTP %d", a.ProviderID, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read body: %w", a.ProviderID, err)
	}

	return parseOpenAICompat(body, a), nil
}

// parseOpenAICompat converts the standard OpenAI /v1/models payload into
// canonical models. Unlike the OpenAI adapter, it does no model filtering —
// every returned model is included as-is.
func parseOpenAICompat(body []byte, a OpenAICompatAdapter) []CanonicalModel {
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	out := make([]CanonicalModel, 0, len(payload.Data))
	for _, e := range payload.Data {
		if e.ID == "" {
			continue
		}
		m := CanonicalModel{
			ID:            e.ID,
			Provider:      a.ProviderID,
			DisplayName:   e.ID,
			Status:        StatusActive,
			Source:        a.ProviderID + ":/v1/models",
			Capabilities:  Capabilities{Streaming: Bool(true)},
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
		}
		out = append(out, m)
	}
	return out
}
