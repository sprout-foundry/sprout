package commands

import (
	"bytes"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
)

func TestGetProviderDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		provider api.ClientType
		want     string
	}{
		{
			name:     "OpenAI",
			provider: api.OpenAIClientType,
			want:     "OpenAI",
		},
		{
			name:     "Z.AI",
			provider: api.ZAIClientType,
			want:     "Z.AI",
		},
		{
			name:     "GLM Coding Plan",
			provider: api.ZAICodingClientType,
			want:     "GLM Coding Plan",
		},
		{
			name:     "DeepInfra",
			provider: api.DeepInfraClientType,
			want:     "DeepInfra",
		},
		{
			name:     "DeepSeek",
			provider: api.DeepSeekClientType,
			want:     "DeepSeek",
		},
		{
			name:     "OpenRouter",
			provider: api.OpenRouterClientType,
			want:     "OpenRouter",
		},
		{
			name:     "Ollama",
			provider: api.OllamaClientType,
			want:     "Ollama",
		},
		{
			name:     "Ollama Local",
			provider: api.OllamaLocalClientType,
			want:     "Ollama Local",
		},
		{
			name:     "Ollama Cloud",
			provider: api.OllamaCloudClientType,
			want:     "Ollama Cloud",
		},
		{
			name:     "LM Studio",
			provider: api.LMStudioClientType,
			want:     "LM Studio",
		},
		{
			name:     "Test (CI/Mock)",
			provider: api.TestClientType,
			want:     "Test (CI/Mock)",
		},
		{
			name:     "unknown provider",
			provider: api.ClientType("custom-provider"),
			want:     "custom-provider",
		},
		{
			name:     "empty provider",
			provider: api.ClientType(""),
			want:     "",
		},
		{
			name:     "provider with special characters",
			provider: api.ClientType("my_custom_provider-v2"),
			want:     "my_custom_provider-v2",
		},
		{
			name:     "provider with numbers",
			provider: api.ClientType("provider123"),
			want:     "provider123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getProviderDisplayName(tt.provider)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderDropdownItemDisplay(t *testing.T) {
	tests := []struct {
		name string
		item providerDropdownItem
		want string
	}{
		{
			name: "OpenAI provider",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "OpenAI",
				available:   true,
			},
			want: "OpenAI",
		},
		{
			name: "Ollama Local provider",
			item: providerDropdownItem{
				provider:    api.OllamaLocalClientType,
				displayName: "Ollama Local",
				available:   true,
			},
			want: "Ollama Local",
		},
		{
			name: "custom provider",
			item: providerDropdownItem{
				provider:    api.ClientType("custom-provider"),
				displayName: "Custom Provider",
				available:   false,
			},
			want: "Custom Provider",
		},
		{
			name: "provider with special characters",
			item: providerDropdownItem{
				provider:    api.ClientType("my_provider-v2"),
				displayName: "My Provider V2",
				available:   true,
			},
			want: "My Provider V2",
		},
		{
			name: "empty display name",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "",
				available:   true,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.Display()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderDropdownItemSearchText(t *testing.T) {
	tests := []struct {
		name string
		item providerDropdownItem
		want string
	}{
		{
			name: "OpenAI provider",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "OpenAI",
				available:   true,
			},
			want: "OpenAI",
		},
		{
			name: "Ollama Local provider",
			item: providerDropdownItem{
				provider:    api.OllamaLocalClientType,
				displayName: "Ollama Local",
				available:   true,
			},
			want: "Ollama Local",
		},
		{
			name: "multiple words",
			item: providerDropdownItem{
				provider:    api.TestClientType,
				displayName: "Test (CI/Mock)",
				available:   true,
			},
			want: "Test (CI/Mock)",
		},
		{
			name: "custom provider",
			item: providerDropdownItem{
				provider:    api.ClientType("custom-provider"),
				displayName: "Custom Provider",
				available:   false,
			},
			want: "Custom Provider",
		},
		{
			name: "empty display name",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "",
				available:   true,
			},
			want: "",
		},
		{
			name: "provider with special characters in display",
			item: providerDropdownItem{
				provider:    api.ClientType("my_provider-v2"),
				displayName: "My Provider V2 (Local)",
				available:   true,
			},
			want: "My Provider V2 (Local)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.SearchText()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderDropdownItemValue(t *testing.T) {
	tests := []struct {
		name string
		item providerDropdownItem
		want api.ClientType
	}{
		{
			name: "OpenAI provider",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "OpenAI",
				available:   true,
			},
			want: api.OpenAIClientType,
		},
		{
			name: "Ollama Local provider",
			item: providerDropdownItem{
				provider:    api.OllamaLocalClientType,
				displayName: "Ollama Local",
				available:   true,
			},
			want: api.OllamaLocalClientType,
		},
		{
			name: "custom provider",
			item: providerDropdownItem{
				provider:    api.ClientType("custom-provider"),
				displayName: "Custom Provider",
				available:   false,
			},
			want: api.ClientType("custom-provider"),
		},
		{
			name: "empty provider",
			item: providerDropdownItem{
				provider:    api.ClientType(""),
				displayName: "",
				available:   true,
			},
			want: api.ClientType(""),
		},
		{
			name: "provider with special characters",
			item: providerDropdownItem{
				provider:    api.ClientType("my_provider-v2"),
				displayName: "My Provider V2",
				available:   true,
			},
			want: api.ClientType("my_provider-v2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.Value()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderDropdownItemAvailableField(t *testing.T) {
	tests := []struct {
		name      string
		item      providerDropdownItem
		wantAvail bool
	}{
		{
			name: "available provider",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "OpenAI",
				available:   true,
			},
			wantAvail: true,
		},
		{
			name: "unavailable provider",
			item: providerDropdownItem{
				provider:    api.ClientType("custom-provider"),
				displayName: "Custom Provider",
				available:   false,
			},
			wantAvail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The available field is not used by any method, but we can still check it exists
			assert.Equal(t, tt.wantAvail, tt.item.available)
		})
	}
}

func TestProviderDropdownItemInterface(t *testing.T) {
	// Test that providerDropdownItem satisfies the expected interface
	item := providerDropdownItem{
		provider:    api.OpenAIClientType,
		displayName: "OpenAI",
		available:   true,
	}

	// Test Display method
	display := item.Display()
	assert.Equal(t, "OpenAI", display)

	// Test SearchText method
	searchText := item.SearchText()
	assert.Equal(t, "OpenAI", searchText)

	// Test Value method
	value := item.Value()
	assert.Equal(t, api.OpenAIClientType, value)

	// Test that value returns correct type
	_, isClientType := value.(api.ClientType)
	assert.True(t, isClientType, "Value() should return api.ClientType")
}

// SP-161g: /custom and /provider are different verbs, so they stay
// separate commands — but an unknown provider is almost always a custom
// endpoint the user has not registered, and the error should say so
// rather than leave them to discover /custom by accident.
func TestProvidersCommand_UnknownProviderPointsAtCustom(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}

	var buf bytes.Buffer
	p := &ProvidersCommand{}
	p.SetOutput(&buf)

	err := p.setProvider("definitely-not-a-provider-xyz", mgr, nil)
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	if !strings.Contains(err.Error(), "/custom add") {
		t.Errorf("error should point at /custom add, got: %v", err)
	}
}

// A user-registered endpoint is marked in the list so it is clear which
// entries came from /custom add and which ship built in.
func TestProvidersCommand_ListMarksCustomProviders(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}

	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = map[string]configuration.CustomProviderConfig{}
		}
		cfg.CustomProviders["my-endpoint"] = configuration.CustomProviderConfig{
			Name:           "my-endpoint",
			Endpoint:       "https://example.invalid/v1",
			ModelName:      "some-model",
			RequiresAPIKey: false,
		}
		return nil
	}); err != nil {
		t.Fatalf("register custom provider: %v", err)
	}

	var buf bytes.Buffer
	p := &ProvidersCommand{}
	p.SetOutput(&buf)

	if err := p.listProviders(mgr); err != nil {
		t.Fatalf("listProviders: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "my-endpoint") {
		t.Errorf("custom provider missing from the list:\n%s", out)
	}
	if !strings.Contains(out, "[custom]") {
		t.Errorf("custom provider not marked as custom:\n%s", out)
	}
}
