// Package agent_api: LM Studio model-list discovery.
//
// LM Studio (≥ 0.3.6) exposes a native REST API at /api/v0/models that reports
// max_context_length and load state per model. Older versions and the OpenAI-
// compatible layer (/v1/models) don't carry context length, so we hit v0
// first and fall back to v1 with a 32k default when v0 is unavailable or
// reports an unloaded/missing-context entry.
//
// Local-only by design — LM Studio's published endpoint is 127.0.0.1:1234.
// LMSTUDIO_BASE_URL is honored for users running a remote LM Studio server.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

const (
	lmStudioDefaultBaseURL = "http://127.0.0.1:1234/v1"
	lmStudioFallbackCtx    = 32768 // used when only OpenAI-compat /v1/models is reachable
)

type lmStudioListModelsWrapper struct{}

func (w *lmStudioListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	baseURL := strings.TrimRight(os.Getenv("LMSTUDIO_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = lmStudioDefaultBaseURL
	}

	client := &http.Client{Timeout: 30 * time.Second}
	nativeBaseURL := strings.TrimSuffix(baseURL, "/v1")
	if v0Models, ok := listLMStudioV0Models(ctx, client, nativeBaseURL); ok {
		return v0Models, nil
	}

	return listLMStudioOpenAIModels(ctx, client, baseURL)
}

// listLMStudioV0Models queries LM Studio's native REST API. Returns the
// resolved list and true on success; (nil, false) signals the caller should
// fall back to the OpenAI-compatible endpoint. A non-200, transport error,
// or decode error all return (nil, false); the OpenAI-compat path is the
// safety net for older LM Studio versions.
func listLMStudioV0Models(ctx context.Context, client *http.Client, nativeBaseURL string) ([]ModelInfo, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeBaseURL+"/api/v0/models", nil)
	if err != nil {
		return nil, false
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	var modelsResp struct {
		Data []struct {
			ID               string `json:"id"`
			State            string `json:"state"`
			MaxContextLength int    `json:"max_context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, false
	}

	models := make([]ModelInfo, 0, len(modelsResp.Data))
	needsFallback := false
	for _, model := range modelsResp.Data {
		if model.State != "loaded" {
			continue
		}
		models = append(models, ModelInfo{
			ID:            model.ID,
			Name:          model.ID,
			Description:   fmt.Sprintf("LM Studio model: %s", model.ID),
			Provider:      "lmstudio",
			ContextLength: model.MaxContextLength,
		})
		needsFallback = needsFallback || model.MaxContextLength == 0
	}

	if !needsFallback {
		return models, true
	}

	// Merge: v1 acts only as a context-length source for v0-confirmed models.
	// v1 entries that v0 did not confirm are intentionally dropped — if a
	// model is loaded in v0 but invisible to v1, we'd rather omit it (and let
	// model_overrides / the user fall back) than surface a model v0 didn't
	// authorize.
	fallbackModels, fallbackErr := listLMStudioOpenAIModels(ctx, client, nativeBaseURL+"/v1")
	if fallbackErr != nil {
		return models, true
	}
	fallbackByID := make(map[string]ModelInfo, len(fallbackModels))
	for _, model := range fallbackModels {
		fallbackByID[model.ID] = model
	}
	resolved := models[:0]
	for _, model := range models {
		if model.ContextLength > 0 {
			resolved = append(resolved, model)
		} else if fallback, ok := fallbackByID[model.ID]; ok {
			resolved = append(resolved, fallback)
		}
	}
	return resolved, true
}

// listLMStudioOpenAIModels queries LM Studio's OpenAI-compat /v1/models
// endpoint. The endpoint doesn't return context length, so each model gets
// a hardcoded 32k fallback. This path is the safety net for older LM Studio
// builds and for the case where v0 is reachable but returns models without
// max_context_length.
func listLMStudioOpenAIModels(ctx context.Context, client *http.Client, baseURL string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to create request", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to fetch LM Studio models", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, agenterrors.NewProviderError("failed to fetch LM Studio models", FormatHTTPResponseError(resp.StatusCode, resp.Header, body), "lmstudio", "")
	}

	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, agenterrors.NewConfig("failed to decode LM Studio models", err)
	}

	models := make([]ModelInfo, 0, len(modelsResp.Data))
	for _, model := range modelsResp.Data {
		models = append(models, ModelInfo{
			ID:            model.ID,
			Name:          model.ID,
			Description:   fmt.Sprintf("LM Studio model: %s", model.ID),
			Provider:      "lmstudio",
			ContextLength: lmStudioFallbackCtx,
		})
	}
	return models, nil
}