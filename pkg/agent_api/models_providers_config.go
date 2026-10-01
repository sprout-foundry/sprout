package api

// models_providers_config.go — the generic config-based model listing:
// the genericConfigListModelsWrapper + its config types (configModelInfo,
// configModels, config, configAuth, customProviderFile), the built-in /
// custom-provider model loaders (loadBuiltInProviderModels,
// loadCustomProviderModels), the custom-provider path resolvers
// (customProviderFilePath, globalCustomProviderFilePath), and the
// OpenAI-compatible model fetch (fetchOpenAICompatibleModels). Split out of
// models_providers.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// genericConfigListModelsWrapper uses provider config for model listing
// This allows providers without dedicated model endpoints to fallback to config-based model info
type genericConfigListModelsWrapper struct {
	providerName string
}

// configModelInfo mirrors providers.ModelInfo for our local use
type configModelInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name,omitempty"`
	Description     string   `json:"description,omitempty"`
	InputCost       float64  `json:"input_cost,omitempty"`
	OutputCost      float64  `json:"output_cost,omitempty"`
	CachedInputCost float64  `json:"cached_input_cost,omitempty"`
	ContextLength   int      `json:"context_length"`
	Tags            []string `json:"tags,omitempty"`
}

// configModels mirrors providers.ModelConfig for our local use
type configModels struct {
	ModelInfo []configModelInfo `json:"model_info,omitempty"`
}

// config mirrors providers.ProviderConfig for our local use
type config struct {
	Endpoint string       `json:"endpoint,omitempty"`
	Auth     configAuth   `json:"auth,omitempty"`
	Name     string       `json:"name,omitempty"`
	Models   configModels `json:"models"`
}

type configAuth struct {
	EnvVar string `json:"env_var,omitempty"`
	Key    string `json:"key,omitempty"`
}

type customProviderFile struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model_name,omitempty"`
	EnvVar   string `json:"env_var,omitempty"`
}

func (w *genericConfigListModelsWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if builtInModels, err := w.loadBuiltInProviderModels(); err == nil {
		return builtInModels, nil
	}

	return w.loadCustomProviderModels(ctx)
}

func (w *genericConfigListModelsWrapper) loadBuiltInProviderModels() ([]ModelInfo, error) {
	var configPath string
	if _, filename, _, ok := runtime.Caller(0); ok {
		configPath = filepath.Join(filepath.Dir(filename), "../agent_providers/configs", w.providerName+".json")
	} else {
		configPath = "pkg/agent_providers/configs/" + w.providerName + ".json"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, agenterrors.NewConfig("failed to read provider config", err)
	}

	var providerConfig config
	if err := json.Unmarshal(data, &providerConfig); err != nil {
		return nil, agenterrors.NewConfig("failed to unmarshal provider config", err)
	}

	models := make([]ModelInfo, len(providerConfig.Models.ModelInfo))
	for i, mi := range providerConfig.Models.ModelInfo {
		models[i] = ModelInfo{
			ID:              mi.ID,
			Name:            mi.Name,
			Description:     mi.Description,
			Provider:        w.providerName,
			InputCost:       mi.InputCost,
			OutputCost:      mi.OutputCost,
			CachedInputCost: mi.CachedInputCost,
			ContextLength:   mi.ContextLength,
			Tags:            mi.Tags,
		}
		if mi.InputCost > 0 || mi.OutputCost > 0 {
			models[i].Cost = (mi.InputCost + mi.OutputCost) / 2.0
		}
	}
	return models, nil
}

func (w *genericConfigListModelsWrapper) loadCustomProviderModels(ctx context.Context) ([]ModelInfo, error) {
	// Try the scoped config dir first (e.g. workspace .sprout/ when isolated
	// config is active). If the provider file isn't there, fall back to the
	// global home dir. This mirrors LoadCustomProviders' merge behavior —
	// without it, /model select fails for providers registered globally when
	// running inside a workspace with isolated config.
	data, scopedErr := os.ReadFile(customProviderFilePath(w.providerName))
	if scopedErr != nil {
		// Try the global home dir as fallback
		globalPath := globalCustomProviderFilePath(w.providerName)
		if globalPath != "" {
			globalData, globalErr := os.ReadFile(globalPath)
			if globalErr == nil {
				data = globalData
				scopedErr = nil
			}
		}
	}
	if scopedErr != nil {
		return nil, agenterrors.NewConfig(fmt.Sprintf("failed to load %s provider config", w.providerName), scopedErr)
	}

	var providerConfig customProviderFile
	if err := json.Unmarshal(data, &providerConfig); err != nil {
		return nil, agenterrors.NewConfig(fmt.Sprintf("failed to parse %s provider config", w.providerName), err)
	}

	models, err := fetchOpenAICompatibleModels(ctx, w.providerName, providerConfig.Endpoint)
	if err == nil && len(models) > 0 {
		for i := range models {
			models[i].Provider = w.providerName
		}
		return models, nil
	}

	if strings.TrimSpace(providerConfig.Model) != "" {
		return []ModelInfo{{
			ID:       strings.TrimSpace(providerConfig.Model),
			Name:     strings.TrimSpace(providerConfig.Model),
			Provider: w.providerName,
		}}, nil
	}

	if err != nil {
		return nil, agenterrors.Wrap(err, fmt.Sprintf("failed to fetch models from %s", w.providerName))
	}
	return nil, agenterrors.NewNotFound(fmt.Sprintf("models for provider %s", w.providerName))
}

func customProviderFilePath(providerName string) string {
	configDir, err := envutil.GetConfigDir()
	if err != nil {
		// Fallback to env-based resolution if GetConfigDir fails
		configRoot := strings.TrimSpace(envutil.GetEnvSimple("CONFIG"))
		if configRoot == "" {
			if homeDir, homeErr := envutil.HomeDir(); homeErr == nil {
				configRoot = filepath.Join(homeDir, ".config", "sprout")
			}
		}
		return filepath.Join(configRoot, "providers", providerName+".json")
	}
	return filepath.Join(configDir, "providers", providerName+".json")
}

// globalCustomProviderFilePath returns the provider config path under the
// user's home ~/.config/sprout/providers/ directory, ignoring SPROUT_CONFIG
// overrides. Used as a fallback when customProviderFilePath (which honors
// SPROUT_CONFIG) doesn't find the file — e.g. when running inside a
// workspace with isolated config but the provider was registered globally.
func globalCustomProviderFilePath(providerName string) string {
	homeDir, err := envutil.HomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".config", "sprout", "providers", providerName+".json")
}

func fetchOpenAICompatibleModels(ctx context.Context, providerName, endpoint string) ([]ModelInfo, error) {
	modelsEndpoint := strings.TrimSuffix(strings.TrimSpace(endpoint), "/chat/completions") + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", modelsEndpoint, nil)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to create request", err)
	}

	var apiKey string
	if resolved, err := credentials.ResolveProviderAPIKey(strings.TrimSpace(providerName), strings.TrimSpace(providerName)); err == nil {
		apiKey = resolved
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, agenterrors.NewNetwork(fmt.Sprintf("failed to fetch models from %s", providerName), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, FormatHTTPResponseError(resp.StatusCode, resp.Header, body)
	}

	var payload struct {
		Data []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name,omitempty"`
			Description   string   `json:"description,omitempty"`
			ContextLength int      `json:"context_length,omitempty"`
			Tags          []string `json:"tags,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, agenterrors.NewConfig("failed to decode models response", err)
	}

	models := make([]ModelInfo, 0, len(payload.Data))
	for _, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		models = append(models, ModelInfo{
			ID:            id,
			Name:          strings.TrimSpace(entry.Name),
			Description:   strings.TrimSpace(entry.Description),
			Provider:      "",
			ContextLength: entry.ContextLength,
			Tags:          entry.Tags,
		})
	}
	return models, nil
}
