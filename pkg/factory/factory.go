package factory

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/providerregistry"
)

// TestClient implements a mock client for CI/testing environments
type TestClient struct {
	model string
	debug bool
}

// Create test client methods
func (t *TestClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	_ = ctx
	return &api.ChatResponse{
		ID:      "test-response-id",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   t.GetModel(),
		Choices: []api.Choice{
			{
				Index: 0,
				Message: api.Message{
					Role:    "assistant",
					Content: "Test response from mock provider",
				},
				FinishReason: "stop",
			},
		},
		Usage: api.ChatUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			EstimatedCost:    0.0,
			Cost:             0.0,
		},
	}, nil
}

func (t *TestClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	// Simple streaming simulation
	callback("Test response from mock provider", "assistant_text")
	return t.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}

func (t *TestClient) CheckConnection() error {
	return nil // Test provider always has good connection
}

func (t *TestClient) SetDebug(debug bool) {
	t.debug = debug
}

func (t *TestClient) SetModel(model string) error {
	t.model = model
	return nil
}

func (t *TestClient) GetModel() string {
	if t.model == "" {
		return "test-model"
	}
	return t.model
}

func (t *TestClient) GetProvider() string {
	return "test"
}

func (t *TestClient) GetModelContextLimit() (int, error) {
	return 4096, nil
}

func (t *TestClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return []api.ModelInfo{
		{Name: "test-model", ContextLength: 4096},
	}, nil
}

func (t *TestClient) SupportsVision() bool {
	return false
}

func (t *TestClient) GetVisionModel() string {
	return ""
}

func (t *TestClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	_ = ctx
	return nil, fmt.Errorf("vision not supported in test provider")
}

func (t *TestClient) GetLastTPS() float64 {
	return 100.0 // Mock TPS
}

func (t *TestClient) GetAverageTPS() float64 {
	return 100.0 // Mock TPS
}

func (t *TestClient) GetTPSStats() map[string]float64 {
	return map[string]float64{"last": 100.0, "average": 100.0}
}

func (t *TestClient) ResetTPSStats() {
	// No-op for test client
}

// Global provider factory instance
var globalProviderFactory *providers.ProviderFactory

// remoteRefreshCancel can be used to cancel the remote refresh goroutine.
var remoteRefreshCancel context.CancelFunc

// init initializes the global provider factory
func init() {
	globalProviderFactory = providers.NewProviderFactory()

	// First, try to load embedded configs (always available)
	if err := globalProviderFactory.LoadEmbeddedConfigs(); err != nil {
		// Log error but continue with fallback methods
		fmt.Printf("Warning: Failed to load embedded provider configs: %v\n", err)
	}

	// Then try to load from filesystem (allows for customization/overriding)
	if err := globalProviderFactory.LoadConfigsFromDirectory("pkg/agent_providers/configs"); err != nil {
		// Try to load from the binary's location (for installed versions)
		if err := globalProviderFactory.LoadConfigsFromDirectory("configs"); err != nil {
			// As a last resort, try to load from current directory
			if err := globalProviderFactory.LoadConfigsFromDirectory("./configs"); err != nil {
				// Expected outside the source tree — provider configs are
				// embedded and already loaded above; these dirs are just an
				// override hook. Only surface the miss under debug.
				if configuration.GetEnvSimple("DEBUG") != "" {
					log.Printf("[debug] failed to load configs from ./configs: %v", err)
				}
			}
		}
	}

	// Let configuration.GetProviderAuthMetadata see remote-loaded
	// providers. The callback reads globalProviderFactory at call time
	// so providers added later by refreshFromRemote are visible without
	// re-registration.
	configuration.SetProviderConfigLookup(func(name string) (string, string, bool) {
		cfg, err := globalProviderFactory.GetProviderConfig(name)
		if err != nil || cfg == nil {
			return "", "", false
		}
		return cfg.Auth.EnvVar, cfg.Auth.Type, true
	})

	// Same idea for the provider-names enumeration paths (onboarding
	// menu, env-var credential sweep, default-provider auto-selection).
	// The closure reads the factory live so providers added later by
	// refreshFromRemote show up in the union returned by
	// configuration.KnownProviderNames().
	configuration.SetProviderNamesLookup(func() []string {
		return globalProviderFactory.GetAvailableProviders()
	})

	// And the friendly display label, sourced from the JSON
	// display_name field. Lets remote-only providers render in
	// onboarding menus / model pickers with their published label
	// instead of the raw lowercase id.
	configuration.SetProviderDisplayNameLookup(func(name string) (string, bool) {
		cfg, err := globalProviderFactory.GetProviderConfig(name)
		if err != nil || cfg == nil || strings.TrimSpace(cfg.DisplayName) == "" {
			return "", false
		}
		return cfg.DisplayName, true
	})

	// Skip network fetch in test binaries to avoid hitting GitHub Pages
	if !inTestBinary() {
		ctx, cancel := context.WithCancel(context.Background())
		remoteRefreshCancel = cancel
		go refreshFromRemote(ctx)
	}
}

// inTestBinary returns true when running inside a Go test binary.
func inTestBinary() bool {
	if len(os.Args) == 0 {
		return false
	}
	name := os.Args[0]
	return strings.HasSuffix(name, ".test") ||
		strings.Contains(name, "/_test/") ||
		strings.Contains(name, `\_test\`) ||
		strings.HasSuffix(name, ".test.exe")
}

// refreshFromRemote fetches all provider configs from the remote registry
// and upserts them into the global provider factory.
func refreshFromRemote(ctx context.Context) {
	results, err := providerregistry.FetchAllProviders(ctx)
	if err != nil {
		log.Printf("[factory] failed to fetch remote providers: %v", err)
		return
	}
	if len(results) == 0 {
		return
	}
	for name, remoteCfg := range results {
		cfg := remoteCfg.ToProviderConfig()
		if cfg == nil {
			continue
		}
		if err := globalProviderFactory.UpsertConfig(name, cfg); err != nil {
			log.Printf("[factory] failed to upsert remote provider %q: %v", name, err)
			continue
		}
	}
	if envutil.GetEnvSimple("DEBUG_FACTORY") != "" {
		count := 0
		for range results {
			count++
		}
		log.Printf("[factory] remote refresh completed: %d providers fetched", count)
	}
}

// GlobalFactory returns the global provider factory instance.
func GlobalFactory() *providers.ProviderFactory {
	return globalProviderFactory
}

// CreateGenericProvider creates a generic provider by name
func CreateGenericProvider(providerName, model string) (api.ClientInterface, error) {
	if config, err := globalProviderFactory.GetProviderConfig(providerName); err == nil {
		configCopy := *config
		resolved, resolveErr := credentials.ResolveProvider(providerName)
		if resolveErr == nil && strings.TrimSpace(resolved.Value) != "" {
			configCopy.Auth.Key = strings.TrimSpace(resolved.Value)
		}
		provider, providerErr := providers.NewGenericProvider(&configCopy)
		if providerErr == nil {
			if model != "" {
				if err := provider.SetModel(model); err != nil {
					return nil, fmt.Errorf("failed to set model: %w", err)
				}
			}
			return provider, nil
		}
	}

	// If not found in generic provider system, try to create from custom provider config
	return CreateCustomProvider(providerName, model)
}

// CreateCustomProvider creates a provider from custom provider configuration
func CreateCustomProvider(providerName, model string) (api.ClientInterface, error) {
	customProviders, err := configuration.LoadCustomProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to load custom providers: %w", err)
	}

	if len(customProviders) == 0 {
		return nil, fmt.Errorf("no custom providers configured")
	}

	customProvider, exists := customProviders[providerName]
	if !exists {
		return nil, fmt.Errorf("custom provider not found: %s", providerName)
	}

	genericConfig, err := customProvider.ToProviderConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build provider config: %w", err)
	}
	if resolved, resolveErr := credentials.ResolveProvider(providerName); resolveErr == nil && strings.TrimSpace(resolved.Value) != "" {
		genericConfig.Auth.Key = strings.TrimSpace(resolved.Value)
	}

	client, err := providers.NewGenericProvider(genericConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic provider: %w", err)
	}
	if model != "" {
		if err := client.SetModel(model); err != nil {
			return nil, fmt.Errorf("failed to set model: %w", err)
		}
	}
	return client, nil
}

// CreateProviderClient is a factory function that creates providers
func CreateProviderClient(clientType api.ClientType, model string) (api.ClientInterface, error) {
	switch clientType {
	case api.OpenAIClientType:
		return CreateGenericProvider("openai", model)
	case api.ChutesClientType:
		// Use the new generic provider system
		return CreateGenericProvider("chutes", model)
	case api.ZAIClientType:
		// Use the new generic provider system
		return CreateGenericProvider("zai", model)
	case api.ZAICodingClientType:
		// GLM Coding Plan uses a dedicated endpoint
		return CreateGenericProvider("zai-coding", model)
	case api.DeepInfraClientType:
		// Use the new generic provider system
		return CreateGenericProvider("deepinfra", model)
	case api.DeepSeekClientType:
		// Use the new generic provider system
		return CreateGenericProvider("deepseek", model)
	case api.OllamaClientType, api.OllamaLocalClientType:
		return api.NewOllamaLocalClient(model)
	case api.OllamaCloudClientType:
		return CreateGenericProvider("ollama-cloud", model)
	case api.OpenRouterClientType:
		// Use the new generic provider system
		return CreateGenericProvider("openrouter", model)
	case api.CerebrasClientType:
		// Use the new generic provider system
		return CreateGenericProvider("cerebras", model)
	case api.LMStudioClientType:
		// Use the new generic provider system
		return CreateGenericProvider("lmstudio", model)
	case api.MistralClientType:
		// Use the new generic provider system
		return CreateGenericProvider("mistral", model)
	case api.MinimaxClientType:
		return CreateGenericProvider("minimax", model)
	case api.TestClientType:
		// Return test/mock client for CI environments
		testClient := &TestClient{model: model}
		if model != "" {
			testClient.SetModel(model)
		}
		return testClient, nil
	default:
		// For custom providers, try to use the generic provider system
		return CreateGenericProvider(string(clientType), model)
	}
}
