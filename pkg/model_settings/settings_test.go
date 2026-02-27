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

func TestResolveCreatorOverrideForQwen3Family(t *testing.T) {
	settings := ResolveModelSettings("qwen/qwen3.5-27b")
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

func TestResolveCreatorOverrideForGptOssFamily(t *testing.T) {
	settings := ResolveModelSettings("openai/gpt-oss-20b")
	if !settings.Known {
		t.Fatalf("expected known settings")
	}
	if settings.SourceType != "creator" {
		t.Fatalf("expected creator source type, got %s", settings.SourceType)
	}
	if settings.Parameters["reasoning_effort"] != "high" {
		t.Fatalf("expected reasoning_effort high, got %#v", settings.Parameters["reasoning_effort"])
	}
}
