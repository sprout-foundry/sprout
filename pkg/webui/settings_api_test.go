//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestSanitizedConfigIncludesSystemPromptText(t *testing.T) {
	cfg := &configuration.Config{
		Version:          "2.0",
		SystemPromptText: "custom system prompt",
	}

	got := sanitizedConfig(cfg)
	if got["system_prompt_text"] != "custom system prompt" {
		t.Fatalf("expected sanitized config to expose system_prompt_text, got %#v", got["system_prompt_text"])
	}
}

func TestApplyPartialSettingsUpdatesSystemPromptText(t *testing.T) {
	cfg := configuration.NewConfig()
	unknown, err := applyPartialSettings(cfg, map[string]interface{}{
		"system_prompt_text": "be stricter",
	})
	if err != nil {
		t.Fatalf("applyPartialSettings returned error: %v", err)
	}
	if len(unknown) > 0 {
		t.Fatalf("applyPartialSettings returned unknown keys: %v", unknown)
	}

	if cfg.SystemPromptText != "be stricter" {
		t.Fatalf("expected system_prompt_text to be updated, got %q", cfg.SystemPromptText)
	}
}
