package modelsettings

import "testing"

func TestNormalizeVariantToBaseModel(t *testing.T) {
	base := normalizeModelKey("openai/gpt-oss-20b_Q4:free")
	if base != "gpt-oss-20b" {
		t.Fatalf("expected normalized base model gpt-oss-20b, got %s", base)
	}
}

func TestResolveCreatorOverrideForMistralFamily(t *testing.T) {
	settings := ResolveModelSettings("mistralai/devstral-2512")
	if !settings.Known {
		t.Fatalf("expected known settings")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["temperature"] != 0.7 {
		t.Fatalf("expected mistral creator temperature 0.7, got %#v", settings.Parameters["temperature"])
	}
}

func TestResolveCreatorOverrideForQwen35ExactModel(t *testing.T) {
	settings := ResolveModelSettings("qwen/qwen3.5-27b")
	if !settings.Known {
		t.Fatalf("expected known settings")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["temperature"] != 0.6 {
		t.Fatalf("expected qwen3.5-27b temperature 0.6, got %#v", settings.Parameters["temperature"])
	}
	if settings.Parameters["top_k"] != 20.0 && settings.Parameters["top_k"] != 20 {
		t.Fatalf("expected qwen3.5-27b top_k 20, got %#v", settings.Parameters["top_k"])
	}
	if settings.Parameters["presence_penalty"] != 0.0 {
		t.Fatalf("expected qwen3.5-27b presence_penalty 0.0, got %#v", settings.Parameters["presence_penalty"])
	}
	if settings.Parameters["repetition_penalty"] != 1.0 {
		t.Fatalf("expected qwen3.5-27b repetition_penalty 1.0, got %#v", settings.Parameters["repetition_penalty"])
	}
}

func TestResolveCreatorOverrideForQwen35AliasAndCreatorOnlyFallback(t *testing.T) {
	settings := ResolveModelSettings("qwen3.5-35-a3b")
	if !settings.Known {
		t.Fatalf("expected known settings for creator-only alias")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["temperature"] != 0.6 {
		t.Fatalf("expected qwen3.5-35-a3b coding temperature 0.6, got %#v", settings.Parameters["temperature"])
	}
	if !settings.Supported["temperature"] {
		t.Fatalf("expected supported temperature for creator-only fallback")
	}
	if settings.Parameters["repetition_penalty"] != 1.0 {
		t.Fatalf("expected qwen3.5-35-a3b repetition_penalty 1.0, got %#v", settings.Parameters["repetition_penalty"])
	}
}

func TestResolveCreatorOverrideForQwen3FamilyDoesNotCaptureQwen35(t *testing.T) {
	settings := ResolveModelSettings("qwen/qwen3-coder-next")
	if !settings.Known {
		t.Fatalf("expected known settings")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["temperature"] != 0.6 {
		t.Fatalf("expected qwen3 family temperature 0.6, got %#v", settings.Parameters["temperature"])
	}
}

func TestResolveCreatorOverrideForMiniMaxFamily(t *testing.T) {
	settings := ResolveModelSettings("minimax/minimax-m2.5")
	if !settings.Known {
		t.Fatalf("expected known settings")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["top_k"] != 40.0 && settings.Parameters["top_k"] != 40 {
		t.Fatalf("expected minimax top_k 40, got %#v", settings.Parameters["top_k"])
	}
}

func TestResolveCreatorOverrideForZAIExactModel(t *testing.T) {
	settings := ResolveModelSettings("z-ai/glm-4.6")
	if !settings.Known {
		t.Fatalf("expected known settings")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["temperature"] != 1.0 {
		t.Fatalf("expected glm-4.6 temperature 1.0, got %#v", settings.Parameters["temperature"])
	}
}

func TestResolveOpenRouterSettingsForGptOssFamily(t *testing.T) {
	settings := ResolveModelSettings("openai/gpt-oss-20b")
	if !settings.Known {
		t.Fatalf("expected known settings")
	}
	if settings.SourceType != "third_party" {
		t.Fatalf("expected third_party source type without creator override, got %s", settings.SourceType)
	}
}

func TestResolveQwen38ThinkingMode(t *testing.T) {
	settings := ResolveModelSettingsForMode("qwen3.8-27b", false)
	if !settings.Known {
		t.Fatalf("expected known settings for qwen3.8-27b")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["temperature"] != 1.0 {
		t.Fatalf("expected qwen3.8 thinking temperature 1.0, got %#v", settings.Parameters["temperature"])
	}
	if settings.Parameters["top_p"] != 0.95 {
		t.Fatalf("expected qwen3.8 thinking top_p 0.95, got %#v", settings.Parameters["top_p"])
	}
	if settings.Parameters["presence_penalty"] != 0.0 {
		t.Fatalf("expected qwen3.8 thinking presence_penalty 0.0, got %#v", settings.Parameters["presence_penalty"])
	}
}

func TestResolveQwen38InstructMode(t *testing.T) {
	settings := ResolveModelSettingsForMode("qwen3.8-27b", true)
	if !settings.Known {
		t.Fatalf("expected known settings for qwen3.8-27b")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["temperature"] != 0.7 {
		t.Fatalf("expected qwen3.8 instruct temperature 0.7, got %#v", settings.Parameters["temperature"])
	}
	if settings.Parameters["top_p"] != 0.8 {
		t.Fatalf("expected qwen3.8 instruct top_p 0.8, got %#v", settings.Parameters["top_p"])
	}
	if settings.Parameters["presence_penalty"] != 1.5 {
		t.Fatalf("expected qwen3.8 instruct presence_penalty 1.5, got %#v", settings.Parameters["presence_penalty"])
	}
}

func TestResolveQwen38FamilyMatchesPrefixVariants(t *testing.T) {
	for _, model := range []string{"qwen3.8-35b-a3b", "qwen/qwen3.8-max"} {
		settings := ResolveModelSettingsForMode(model, false)
		if !settings.Known {
			t.Fatalf("expected known settings for %s", model)
		}
		if settings.Parameters["temperature"] != 1.0 {
			t.Fatalf("expected %s thinking temperature 1.0, got %#v", model, settings.Parameters["temperature"])
		}
	}
}

func TestResolveModelSettingsDefaultsToThinkingMode(t *testing.T) {
	if got := ResolveModelSettings("qwen3.8-27b").Parameters["temperature"]; got != 1.0 {
		t.Fatalf("expected default ResolveModelSettings to use thinking mode temp 1.0, got %#v", got)
	}
}

func TestResolveQwen36CodingMode(t *testing.T) {
	// Qwen3.6 family uses the creator's "precise coding tasks" set as its
	// thinking-mode default (temp 0.6) and the instruct set when non-thinking.
	settings := ResolveModelSettingsForMode("qwen3.6-27b", false)
	if !settings.Known {
		t.Fatalf("expected known settings for qwen3.6-27b")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["temperature"] != 0.6 {
		t.Fatalf("expected qwen3.6 coding temperature 0.6, got %#v", settings.Parameters["temperature"])
	}
	if settings.Parameters["top_p"] != 0.95 {
		t.Fatalf("expected qwen3.6 coding top_p 0.95, got %#v", settings.Parameters["top_p"])
	}
	if settings.Parameters["presence_penalty"] != 0.0 {
		t.Fatalf("expected qwen3.6 coding presence_penalty 0.0, got %#v", settings.Parameters["presence_penalty"])
	}

	instruct := ResolveModelSettingsForMode("qwen3.6-27b", true)
	if instruct.Parameters["temperature"] != 0.7 {
		t.Fatalf("expected qwen3.6 instruct temperature 0.7, got %#v", instruct.Parameters["temperature"])
	}
	if instruct.Parameters["presence_penalty"] != 1.5 {
		t.Fatalf("expected qwen3.6 instruct presence_penalty 1.5, got %#v", instruct.Parameters["presence_penalty"])
	}
}

func TestResolveQwen36FamilyDoesNotCaptureQwen35Or38(t *testing.T) {
	if s := ResolveModelSettingsForMode("qwen3.5-27b", false); s.SourceType != "creator" || s.Parameters["temperature"] != 0.6 {
		t.Fatalf("qwen3.5-27b should resolve to qwen3.5 profile, got src=%s temp=%#v", s.SourceType, s.Parameters["temperature"])
	}
	if s := ResolveModelSettingsForMode("qwen3.8-27b", false); s.SourceType != "creator" || s.Parameters["temperature"] != 1.0 {
		t.Fatalf("qwen3.8-27b should resolve to qwen3.8 profile, got src=%s temp=%#v", s.SourceType, s.Parameters["temperature"])
	}
}

func TestResolveQwen25CoderPrefixBeatsGeneric(t *testing.T) {
	// qwen2.5-coder-* must resolve to the coder profile (repetition_penalty 1.1),
	// not be shadowed by the generic qwen2.5-family prefix (repetition_penalty 1.05).
	coder := ResolveModelSettingsForMode("qwen2.5-coder-32b", false)
	if coder.Parameters["repetition_penalty"] != 1.1 {
		t.Fatalf("expected qwen2.5-coder repetition_penalty 1.1, got %#v", coder.Parameters["repetition_penalty"])
	}
	generic := ResolveModelSettingsForMode("qwen2.5-7b-instruct", false)
	if generic.Parameters["repetition_penalty"] != 1.05 {
		t.Fatalf("expected qwen2.5 generic repetition_penalty 1.05, got %#v", generic.Parameters["repetition_penalty"])
	}
}
