package modelcontract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const openAIModelsURL = "https://api.openai.com/v1/models"

// OpenAIAdapter normalizes OpenAI's /v1/models into canonical models. Unlike
// DeepInfra/OpenRouter, OpenAI's listing requires an API key and returns only
// model IDs (no capabilities/context/pricing). The adapter therefore enriches
// each model from a Reference catalog (typically built from OpenRouter, which
// lists the same models as `openai/<id>`): capabilities and context are copied
// with confidence, pricing is borrowed but flagged Estimated since OpenAI
// pricing is account-specific.
type OpenAIAdapter struct {
	APIKey     string // required — OpenAI's model list is authenticated
	HTTPClient *http.Client
	Reference  *ReferenceCatalog // optional enrichment source
}

func (a OpenAIAdapter) Provider() string { return "openai" }

func (a OpenAIAdapter) ListModels(ctx context.Context) ([]CanonicalModel, error) {
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openAIModelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if a.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: fetch models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: fetch models: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read body: %w", err)
	}
	return parseOpenAI(body, a.Reference), nil
}

// parseOpenAI converts OpenAI's /v1/models payload into canonical models,
// keeping only chat models and enriching from ref when available.
func parseOpenAI(body []byte, ref *ReferenceCatalog) []CanonicalModel {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	out := make([]CanonicalModel, 0, len(payload.Data))
	for _, e := range payload.Data {
		if !isOpenAIChatModel(e.ID) {
			continue
		}
		m := CanonicalModel{
			ID:          e.ID,
			Provider:    "openai",
			DisplayName: e.ID,
			Status:      StatusActive,
			Source:      "openai:/v1/models",
			// OpenAI's chat APIs stream; other capabilities come from the
			// reference (or stay unknown).
			Capabilities: Capabilities{Streaming: Bool(true)},
		}
		if ref != nil {
			if r, ok := ref.Lookup("openai", e.ID); ok {
				m = EnrichFromReference(m, r)
				m.Source = "openai:/v1/models + openrouter-reference"
			}
		}
		out = append(out, m)
	}
	return out
}

// isOpenAIChatModel filters OpenAI's model list (which mixes in embeddings,
// audio, image, and moderation models) down to chat/agentic models by ID.
func isOpenAIChatModel(id string) bool {
	s := strings.ToLower(id)
	for _, bad := range []string{
		"embedding", "whisper", "tts", "dall-e", "audio", "image",
		"moderation", "transcribe", "realtime", "search", "babbage",
		"davinci", "ada", "curie", "codex",
	} {
		if strings.Contains(s, bad) {
			return false
		}
	}
	return strings.HasPrefix(s, "gpt-") || strings.HasPrefix(s, "chatgpt") ||
		strings.HasPrefix(s, "o1") || strings.HasPrefix(s, "o3") || strings.HasPrefix(s, "o4")
}
