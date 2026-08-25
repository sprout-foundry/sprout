package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// fetchVLLMContextLimit queries vLLM's /get_model_info endpoint for max_model_len.
// Returns the context limit and true on success, 0 and false on failure.
func fetchVLLMContextLimit(ctx context.Context, client *http.Client, endpoint, authToken string) (int, bool) {
	base := baseURLFromEndpoint(endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/get_model_info", nil)
	if err != nil {
		return 0, false
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, false
	}

	var body struct {
		MaxModelLen           int `json:"max_model_len"`
		MaxPositionEmbeddings int `json:"max_position_embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, false
	}

	if body.MaxModelLen > 0 {
		return body.MaxModelLen, true
	}
	if body.MaxPositionEmbeddings > 0 {
		return body.MaxPositionEmbeddings, true
	}
	return 0, false
}

// fetchLlamaCPPContextLimit queries llama.cpp's /props endpoint for n_ctx.
// Returns the context limit and true on success, 0 and false on failure.
func fetchLlamaCPPContextLimit(ctx context.Context, client *http.Client, endpoint, authToken string) (int, bool) {
	base := baseURLFromEndpoint(endpoint)

	for _, path := range []string{"/props", "/v1/props"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			continue
		}
		if authToken != "" {
			req.Header.Set("Authorization", "Bearer "+authToken)
		}

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var body struct {
			DefaultGenerationSettings struct {
				NCtx int `json:"n_ctx"`
			} `json:"default_generation_settings"`
			NCtx int `json:"n_ctx"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			continue
		}

		if body.DefaultGenerationSettings.NCtx > 0 {
			return body.DefaultGenerationSettings.NCtx, true
		}
		if body.NCtx > 0 {
			return body.NCtx, true
		}
	}
	return 0, false
}

// rawModelEntry holds the fields we need from the OpenAI-compat /models
// response. Callers enrich these with backend-specific context data.
type rawModelEntry struct {
	ID            string
	ContextLength int
	MaxModelLen   int
}

func fetchRawModelList(ctx context.Context, client *http.Client, endpoint, authToken string) ([]rawModelEntry, bool) {
	modelsEndpoint := strings.TrimSuffix(endpoint, "/chat/completions") + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsEndpoint, nil)
	if err != nil {
		return nil, false
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	var modelsResponse struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length,omitempty"`
			MaxModelLen   int    `json:"max_model_len,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, false
	}

	entries := make([]rawModelEntry, 0, len(modelsResponse.Data))
	for _, m := range modelsResponse.Data {
		entries = append(entries, rawModelEntry{
			ID:            m.ID,
			ContextLength: m.ContextLength,
			MaxModelLen:   m.MaxModelLen,
		})
	}
	return entries, len(entries) > 0
}

// parseContextLengthFromRaw safely extracts the effective context length
// from a raw model entry, preferring context_length and falling back to
// max_model_len.
func parseContextLengthFromRaw(e rawModelEntry) int {
	if e.ContextLength > 0 {
		return e.ContextLength
	}
	if e.MaxModelLen > 0 {
		return e.MaxModelLen
	}
	return 0
}
