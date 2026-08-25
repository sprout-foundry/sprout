package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// BackendType identifies the serving backend for model discovery.
type BackendType string

const (
	BackendAuto     BackendType = "auto"
	BackendOpenAI   BackendType = "openai"
	BackendVLLM     BackendType = "vllm"
	BackendLlamaCPP BackendType = "llamacpp"
)

const backendProbeTimeout = 2 * time.Second

// baseURLFromEndpoint strips /chat/completions and optional /v1 from the
// endpoint to reach the server root, where backend-specific endpoints live.
//
//	"http://host:8000/v1/chat/completions" → "http://host:8000"
//	"http://host:8080/chat/completions"    → "http://host:8080"
func baseURLFromEndpoint(endpoint string) string {
	base := strings.TrimSuffix(endpoint, "/chat/completions")
	base = strings.TrimSuffix(base, "/v1")
	return base
}

// detectBackend probes the endpoint to determine the serving backend type.
// It tries vLLM and llama.cpp-specific endpoints in order, falling back to
// "openai" if neither responds. Each probe uses a 2-second timeout and
// silently fails — any network or parse error yields BackendOpenAI.
func detectBackend(ctx context.Context, client *http.Client, endpoint string) BackendType {
	base := baseURLFromEndpoint(endpoint)

	if isVLLM(ctx, client, base) {
		return BackendVLLM
	}
	if isLlamaCPP(ctx, client, base) {
		return BackendLlamaCPP
	}
	return BackendOpenAI
}

// isVLLM probes /get_model_info, a vLLM-specific endpoint that returns
// max_model_len and model_name.
func isVLLM(ctx context.Context, client *http.Client, base string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, backendProbeTimeout)
	defer cancel()

	for _, path := range []string{"/get_model_info"} {
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+path, nil)
		if err != nil {
			continue
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
			MaxModelLen           int    `json:"max_model_len"`
			ModelName             string `json:"model_name"`
			MaxPositionEmbeddings int    `json:"max_position_embeddings"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			continue
		}
		if body.MaxModelLen > 0 || body.ModelName != "" || body.MaxPositionEmbeddings > 0 {
			return true
		}
	}
	return false
}

// isLlamaCPP probes /props, a llama.cpp server endpoint that returns
// default_generation_settings with n_ctx.
func isLlamaCPP(ctx context.Context, client *http.Client, base string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, backendProbeTimeout)
	defer cancel()

	for _, path := range []string{"/props", "/v1/props"} {
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+path, nil)
		if err != nil {
			continue
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
		if body.DefaultGenerationSettings.NCtx > 0 || body.NCtx > 0 {
			return true
		}
	}
	return false
}
