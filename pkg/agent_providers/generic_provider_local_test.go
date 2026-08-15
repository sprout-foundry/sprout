package providers

import (
	"errors"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestIsLocalProvider(t *testing.T) {
	tests := []struct {
		name     string
		config   *ProviderConfig
		expected bool
	}{
		{
			name: "sprout-local by name",
			config: &ProviderConfig{
				Name:     "sprout-local",
				Endpoint: "http://127.0.0.1:18081/v1/chat/completions",
			},
			expected: true,
		},
		{
			name: "sprout-local by endpoint port",
			config: &ProviderConfig{
				Name:     "custom-local",
				Endpoint: "http://localhost:18081/v1/chat/completions",
			},
			expected: true,
		},
		{
			name: "non-local cloud provider",
			config: &ProviderConfig{
				Name:     "openrouter",
				Endpoint: "https://openrouter.ai/api/v1/chat/completions",
			},
			expected: false,
		},
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name: "other localhost port (e.g. ollama)",
			config: &ProviderConfig{
				Name:     "ollama-local",
				Endpoint: "http://127.0.0.1:11434/v1/chat/completions",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &GenericProvider{config: tt.config}
			if got := p.isLocalProvider(); got != tt.expected {
				t.Errorf("isLocalProvider() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTryLocalServerRecovery_NoHook(t *testing.T) {
	original := LocalServerHook
	LocalServerHook = nil
	defer func() { LocalServerHook = original }()

	p := &GenericProvider{
		config: &ProviderConfig{
			Name:     "sprout-local",
			Endpoint: "http://127.0.0.1:18081/v1/chat/completions",
		},
	}

	if p.tryLocalServerRecovery() {
		t.Error("should return false when hook is nil")
	}
}

func TestTryLocalServerRecovery_NotLocal(t *testing.T) {
	hookCalled := false
	original := LocalServerHook
	LocalServerHook = func(string) error {
		hookCalled = true
		return nil
	}
	defer func() { LocalServerHook = original }()

	p := &GenericProvider{
		config: &ProviderConfig{
			Name:     "openrouter",
			Endpoint: "https://openrouter.ai/api/v1/chat/completions",
		},
	}

	if p.tryLocalServerRecovery() {
		t.Error("should return false for non-local provider")
	}
	if hookCalled {
		t.Error("hook should not be called for non-local provider")
	}
}

func TestTryLocalServerRecovery_HookError(t *testing.T) {
	original := LocalServerHook
	LocalServerHook = func(string) error {
		return errors.New("server not available")
	}
	defer func() { LocalServerHook = original }()

	p := &GenericProvider{
		config: &ProviderConfig{
			Name:     "sprout-local",
			Endpoint: "http://127.0.0.1:18081/v1/chat/completions",
		},
	}

	if p.tryLocalServerRecovery() {
		t.Error("should return false when hook returns error")
	}
}

func TestTryLocalServerRecovery_HookSuccess(t *testing.T) {
	activityCalled := false
	originalHook := LocalServerHook
	originalActivity := LocalActivityHook
	LocalServerHook = func(string) error { return nil }
	LocalActivityHook = func() { activityCalled = true }
	defer func() {
		LocalServerHook = originalHook
		LocalActivityHook = originalActivity
	}()

	p := &GenericProvider{
		config: &ProviderConfig{
			Name:     "sprout-local",
			Endpoint: "http://127.0.0.1:18081/v1/chat/completions",
		},
	}

	if !p.tryLocalServerRecovery() {
		t.Error("should return true when hook succeeds")
	}
	if !activityCalled {
		t.Error("activity hook should have been called on success")
	}
}

func TestProviderDisplayNames_HasSproutLocal(t *testing.T) {
	names := ProviderDisplayNames()
	name, ok := names["sprout-local"]
	if !ok {
		t.Fatal("sprout-local not found in ProviderDisplayNames()")
	}
	if name != "Local (Offline)" {
		t.Errorf("expected 'Local (Offline)', got %q", name)
	}
}

func TestGetProviderName_SproutLocal(t *testing.T) {
	name := api.GetProviderName("sprout-local")
	if name != "Local (Offline)" {
		t.Errorf("expected 'Local (Offline)', got %q", name)
	}
}
