package modelcontract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const openRouterModelsURL = "https://openrouter.ai/api/v1/models"

// OpenRouterAdapter normalizes OpenRouter's /api/v1/models (public, keyless)
// into canonical models. OpenRouter is also the richest cross-provider source,
// so its output typically seeds the ReferenceCatalog other adapters borrow from.
type OpenRouterAdapter struct {
	HTTPClient *http.Client
}

func (a OpenRouterAdapter) Provider() string { return "openrouter" }

func (a OpenRouterAdapter) ListModels(ctx context.Context) ([]CanonicalModel, error) {
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterModelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("openrouter: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter: fetch models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter: fetch models: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openrouter: read body: %w", err)
	}
	return parseOpenRouter(body)
}

type openRouterModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt         string `json:"prompt"`
		Completion     string `json:"completion"`
		InputCacheRead string `json:"input_cache_read"`
	} `json:"pricing"`
	TopProvider struct {
		ContextLength       int `json:"context_length"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
	SupportedParameters []string `json:"supported_parameters"`
}

func parseOpenRouter(body []byte) ([]CanonicalModel, error) {
	var payload struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("openrouter: decode models: %w", err)
	}

	out := make([]CanonicalModel, 0, len(payload.Data))
	for _, e := range payload.Data {
		if e.ID == "" {
			continue
		}
		ctx := e.ContextLength
		if ctx == 0 {
			ctx = e.TopProvider.ContextLength
		}
		m := CanonicalModel{
			ID:              e.ID,
			Provider:        "openrouter",
			DisplayName:     e.Name,
			Description:     e.Description,
			ContextWindow:   ctx,
			MaxOutputTokens: e.TopProvider.MaxCompletionTokens,
			Status:          StatusActive,
			Source:          "openrouter:/api/v1/models",
			Capabilities: Capabilities{
				// supported_parameters is OpenRouter's authoritative capability
				// list, so absence is a known-false.
				Tools:            Bool(tagsAny(e.SupportedParameters, "tools", "tool_choice")),
				Reasoning:        Bool(tagsAny(e.SupportedParameters, "reasoning", "include_reasoning")),
				StructuredOutput: Bool(tagsAny(e.SupportedParameters, "structured_outputs", "response_format")),
				Vision:           Bool(tagsAny(e.Architecture.InputModalities, "image")),
				Streaming:        Bool(true),
			},
			InputModalities:  e.Architecture.InputModalities,
			OutputModalities: e.Architecture.OutputModalities,
		}
		in, inOK := parsePerTokenUSD(e.Pricing.Prompt)
		out2, outOK := parsePerTokenUSD(e.Pricing.Completion)
		if inOK || outOK {
			p := &Pricing{InputPerMTok: in, OutputPerMTok: out2, Currency: "USD"}
			// OpenRouter exposes a distinct cached-input rate for models
			// whose underlying provider supports prompt caching (Anthropic,
			// DeepSeek, Google, …). "0" means free, which is a valid price.
			if cached, ok := parsePerTokenUSD(e.Pricing.InputCacheRead); ok {
				p.CachedPerMTok = cached
			}
			m.Pricing = p
		}
		out = append(out, m)
	}
	return out, nil
}

// parsePerTokenUSD parses an OpenRouter per-token USD price string into USD per
// million tokens. Returns ok=false for empty/unparseable values; "0" is a valid
// (free) price.
func parsePerTokenUSD(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v * 1e6, true
}
