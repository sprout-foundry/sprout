package main

// PricingEntry is the verified pricing for a single model.
type PricingEntry struct {
	ID             string  `json:"id"`
	InputPerMTok   float64 `json:"input_per_mtok"`
	OutputPerMTok  float64 `json:"output_per_mtok"`
	CachedPerMTok  float64 `json:"cached_per_mtok,omitempty"`
}

// ProviderManifest is the verified pricing for a provider's models.
type ProviderManifest struct {
	// Source is the official docs URL where pricing was verified.
	Source string `json:"source"`
	// LastVerified is when a human or agent last checked the source.
	LastVerified string         `json:"last_verified"`
	Models       []PricingEntry `json:"models"`
}

// manifests is the authoritative pricing truth keyed by provider ID.
// When providers change pricing, a human updates this manifest and the audit
// catches any config drift. Only providers with stable, documented pricing
// are included — providers with volatile or undocumented pricing (openrouter,
// deepinfra, chutes, ollama-cloud) are omitted.
var manifests = map[string]ProviderManifest{
	"deepseek": {
		Source:       "https://api-docs.deepseek.com/quick_start/pricing",
		LastVerified: "2026-07-26",
		Models: []PricingEntry{
			{ID: "deepseek-v4-flash", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
			{ID: "deepseek-v4-pro", InputPerMTok: 0.435, OutputPerMTok: 0.87, CachedPerMTok: 0.003625},
			{ID: "deepseek-v4-flash-max", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
			{ID: "deepseek-chat", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
			{ID: "deepseek-reasoner", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
			{ID: "deepseek-v3", InputPerMTok: 0.27, OutputPerMTok: 1.1, CachedPerMTok: 0.027},
			{ID: "deepseek-coder", InputPerMTok: 0.14, OutputPerMTok: 0.28, CachedPerMTok: 0.0028},
		},
	},

	"zai": {
		Source:       "https://docs.z.ai/devpack/overview",
		LastVerified: "2026-07-26",
		Models: []PricingEntry{
			{ID: "GLM-4.5", InputPerMTok: 0.6, OutputPerMTok: 2.2},
			{ID: "GLM-4.5-air", InputPerMTok: 0.2, OutputPerMTok: 1.1},
			{ID: "GLM-4.6", InputPerMTok: 0.6, OutputPerMTok: 2.2},
			{ID: "glm-4.5v", InputPerMTok: 0.6, OutputPerMTok: 1.8},
			{ID: "glm-4.6v", InputPerMTok: 0.3, OutputPerMTok: 0.9},
			{ID: "glm-4.7", InputPerMTok: 0.6, OutputPerMTok: 2.2},
			{ID: "glm-5", InputPerMTok: 1.0, OutputPerMTok: 3.2},
			{ID: "glm-5-turbo", InputPerMTok: 1.2, OutputPerMTok: 4.0},
			{ID: "glm-5.1", InputPerMTok: 1.4, OutputPerMTok: 4.4},
			{ID: "glm-5.2", InputPerMTok: 1.4, OutputPerMTok: 4.4},
			{ID: "glm-5v-turbo", InputPerMTok: 1.2, OutputPerMTok: 4.0},
		},
	},

	"openai": {
		Source:       "https://platform.openai.com/docs/pricing",
		LastVerified: "2026-07-26",
		Models: []PricingEntry{
			{ID: "gpt-5", InputPerMTok: 1.25, OutputPerMTok: 10.0},
			{ID: "gpt-5-mini", InputPerMTok: 0.25, OutputPerMTok: 2.0},
			{ID: "gpt-5-nano", InputPerMTok: 0.05, OutputPerMTok: 0.4},
			{ID: "gpt-5-pro", InputPerMTok: 15.0, OutputPerMTok: 120.0},
			{ID: "gpt-4o", InputPerMTok: 2.5, OutputPerMTok: 10.0},
			{ID: "gpt-4o-mini", InputPerMTok: 0.15, OutputPerMTok: 0.6},
			{ID: "gpt-4.1", InputPerMTok: 2.0, OutputPerMTok: 8.0},
			{ID: "gpt-4.1-mini", InputPerMTok: 0.4, OutputPerMTok: 1.6},
			{ID: "gpt-4.1-nano", InputPerMTok: 0.1, OutputPerMTok: 0.4},
			{ID: "o3", InputPerMTok: 2.0, OutputPerMTok: 8.0},
			{ID: "o3-mini", InputPerMTok: 1.1, OutputPerMTok: 4.4},
			{ID: "o4-mini", InputPerMTok: 1.1, OutputPerMTok: 4.4},
		},
	},

	"minimax": {
		Source:       "https://platform.minimax.io/docs/api-reference/api-overview",
		LastVerified: "2026-07-26",
		Models: []PricingEntry{
			{ID: "MiniMax-M2", InputPerMTok: 0.15, OutputPerMTok: 1.15},
			{ID: "MiniMax-M2.1", InputPerMTok: 0.1, OutputPerMTok: 0.1},
			{ID: "MiniMax-M2.5", InputPerMTok: 0.15, OutputPerMTok: 1.15},
			{ID: "MiniMax-M2.7", InputPerMTok: 0.15, OutputPerMTok: 1.15},
		},
	},

	"mistral": {
		Source:       "https://docs.mistral.ai/",
		LastVerified: "2026-07-26",
		Models: []PricingEntry{
			{ID: "mistral-large-latest", InputPerMTok: 2.0, OutputPerMTok: 6.0},
			{ID: "mistral-medium-latest", InputPerMTok: 0.4, OutputPerMTok: 2.0},
			{ID: "mistral-small-latest", InputPerMTok: 0.15, OutputPerMTok: 0.6},
			{ID: "codestral-latest", InputPerMTok: 0.3, OutputPerMTok: 0.9},
		},
	},
}
