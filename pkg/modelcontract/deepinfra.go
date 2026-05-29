package modelcontract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const deepInfraModelsURL = "https://api.deepinfra.com/models/list"

// DeepInfraAdapter normalizes DeepInfra's native /models/list (public, keyless)
// into canonical models. That endpoint — unlike the OpenAI-compatible
// /v1/openai/models — reports context window, per-token pricing, capability
// tags, type, and a deprecated flag.
type DeepInfraAdapter struct {
	HTTPClient *http.Client
}

func (a DeepInfraAdapter) Provider() string { return "deepinfra" }

func (a DeepInfraAdapter) ListModels(ctx context.Context) ([]CanonicalModel, error) {
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deepInfraModelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deepinfra: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepinfra: fetch models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepinfra: fetch models: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("deepinfra: read body: %w", err)
	}
	return parseDeepInfra(body)
}

type deepInfraModel struct {
	ModelName    string `json:"model_name"`
	Type         string `json:"type"`
	ReportedType string `json:"reported_type"`
	Description  string `json:"description"`
	// DeepInfra reports deprecated as 0/1 or a timestamp number (not a bool),
	// so decode loosely and interpret truthiness.
	Deprecated any      `json:"deprecated"`
	MaxTokens  int      `json:"max_tokens"`
	Tags       []string `json:"tags"`
	ReplacedBy string   `json:"replaced_by"`
	Pricing    struct {
		Type                string  `json:"type"`
		CentsPerInputToken  float64 `json:"cents_per_input_token"`
		CentsPerOutputToken float64 `json:"cents_per_output_token"`
	} `json:"pricing"`
}

// parseDeepInfra converts the native /models/list payload into canonical models,
// keeping only non-deprecated text-generation models.
func parseDeepInfra(body []byte) ([]CanonicalModel, error) {
	var entries []deepInfraModel
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("deepinfra: decode models: %w", err)
	}

	out := make([]CanonicalModel, 0, len(entries))
	for _, e := range entries {
		if truthy(e.Deprecated) || e.ModelName == "" {
			continue
		}
		t := e.ReportedType
		if t == "" {
			t = e.Type
		}
		if t != "text-generation" {
			continue
		}

		vision := tagsAny(e.Tags, "multimodal", "ocr", "input-image", "input-video")
		m := CanonicalModel{
			ID:            e.ModelName,
			Provider:      "deepinfra",
			DisplayName:   e.ModelName,
			Description:   e.Description,
			ContextWindow: e.MaxTokens,
			Status:        StatusActive,
			ReplacedBy:    e.ReplacedBy,
			Source:        "deepinfra:/models/list",
			Capabilities: Capabilities{
				// DeepInfra tags models comprehensively, so absence of a tag is
				// treated as a known-false rather than unknown.
				Tools:            Bool(tagsAny(e.Tags, "tools")),
				Vision:           Bool(vision),
				Reasoning:        Bool(tagsAny(e.Tags, "reasoning")),
				StructuredOutput: Bool(tagsAny(e.Tags, "structured-output", "json")),
				Streaming:        Bool(true), // DeepInfra's OpenAI-compatible API streams
			},
			InputModalities:  modalities(vision),
			OutputModalities: []string{"text"},
		}
		if e.Pricing.Type == "tokens" && (e.Pricing.CentsPerInputToken > 0 || e.Pricing.CentsPerOutputToken > 0) {
			// cents-per-token → USD per million tokens: ×1e6 tokens ÷100 cents = ×1e4.
			m.Pricing = &Pricing{
				InputPerMTok:  e.Pricing.CentsPerInputToken * 1e4,
				OutputPerMTok: e.Pricing.CentsPerOutputToken * 1e4,
				Currency:      "USD",
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// truthy interprets a loosely-typed JSON value (bool, number, or string) as a
// boolean — used for fields like DeepInfra's `deprecated`, which may arrive as
// 0/1, a timestamp, or a bool depending on the entry.
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != "" && x != "false" && x != "0"
	default:
		return false
	}
}

// tagsAny reports whether tags contains any of the given values.
func tagsAny(tags []string, want ...string) bool {
	for _, t := range tags {
		for _, w := range want {
			if t == w {
				return true
			}
		}
	}
	return false
}

// modalities returns the input modality list for a text model.
func modalities(vision bool) []string {
	if vision {
		return []string{"text", "image"}
	}
	return []string{"text"}
}
