//go:build !js

package webui

// onboarding_presentation.go — the webui onboarding presentation layer:
// the onboardingProvider / onboardingEnvironment /
// onboardingProviderPresentation types, the provider presentation table +
// ordering (onboardingProviderPresentations, onboardingProviderOrder), and
// applyOnboardingPresentation. Split out of onboarding_api.go.

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

type onboardingProvider struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Models              []string `json:"models"`
	RequiresAPIKey      bool     `json:"requires_api_key"`
	HasCredential       bool     `json:"has_credential"`
	Recommended         bool     `json:"recommended"`
	Description         string   `json:"description"`
	SetupHint           string   `json:"setup_hint"`
	DocsURL             string   `json:"docs_url"`
	SignupURL           string   `json:"signup_url"`
	APIKeyLabel         string   `json:"api_key_label"`
	APIKeyHelp          string   `json:"api_key_help"`
	RecommendedModel    string   `json:"recommended_model"`
	RecommendedModelWhy string   `json:"recommended_model_why"`
}

type onboardingEnvironment struct {
	RuntimePlatform     string   `json:"runtime_platform"`
	HostPlatform        string   `json:"host_platform"`
	BackendMode         string   `json:"backend_mode"`
	HasWSL              bool     `json:"has_wsl"`
	HasGitBash          bool     `json:"has_git_bash"`
	RecommendedTerminal string   `json:"recommended_terminal"`
	ActiveDistro        string   `json:"active_distro"`
	WslDistros          []string `json:"wsl_distros"`
}

type onboardingProviderPresentation struct {
	Description         string
	SetupHint           string
	DocsURL             string
	SignupURL           string
	APIKeyLabel         string
	APIKeyHelp          string
	Recommended         bool
	RecommendedPrefixes []string
	RecommendedModelWhy string
}

var onboardingProviderPresentations = map[string]onboardingProviderPresentation{
	"zai": {
		Description:         "GLM models through the Z.AI API platform. Z.AI also offers a GLM Coding Plan subscription and remote MCP services.",
		SetupHint:           "Use either a standard Z.AI API key or, if you already have one, a GLM Coding Plan setup.",
		DocsURL:             "https://docs.z.ai/devpack/overview",
		SignupURL:           "https://platform.z.ai/",
		APIKeyLabel:         "Z.AI API Key",
		APIKeyHelp:          "Create a key in the Z.AI API platform. Coding Plan subscriptions are separate from normal API billing.",
		Recommended:         false,
		RecommendedPrefixes: []string{"glm-5", "glm-4.7", "glm-4.6", "glm-4.5-air"},
		RecommendedModelWhy: "Prefer a current GLM coding model if one is listed for your account.",
	},
	"minimax": {
		Description:         "Strong coding-oriented provider with a dedicated coding plan and large context windows.",
		SetupHint:           "MiniMax supports both normal API keys and coding-plan keys. Start with M2.5 if available.",
		DocsURL:             "https://platform.minimax.io/docs/api-reference/api-overview",
		SignupURL:           "https://platform.minimax.io/",
		APIKeyLabel:         "MiniMax API Key",
		APIKeyHelp:          "Create either a pay-as-you-go key or a coding-plan key in the MiniMax platform.",
		Recommended:         true,
		RecommendedPrefixes: []string{"minimax-m2.5", "minimax-m2.1", "minimax-m2"},
		RecommendedModelWhy: "Prefer the newest M2.x coding model that your account exposes.",
	},
	"openrouter": {
		Description:         "Unified gateway to many model families behind one API key and one OpenAI-compatible endpoint.",
		SetupHint:           "Best when you want broad model choice and easy switching without managing separate vendor accounts.",
		DocsURL:             "https://openrouter.ai/",
		SignupURL:           "https://openrouter.ai/keys",
		APIKeyLabel:         "OpenRouter API Key",
		APIKeyHelp:          "Create an API key in OpenRouter, then choose a coding-focused model from the list below.",
		Recommended:         false,
		RecommendedPrefixes: []string{"qwen/qwen3-coder", "deepseek/deepseek-chat", "z-ai/glm", "google/gemini-2.5-pro"},
		RecommendedModelWhy: "Prefer a coding-focused or reasoning-heavy model instead of a generic default.",
	},
	"deepinfra": {
		Description:         "Simple hosted inference with broad open-model coverage and straightforward OpenAI-compatible APIs.",
		SetupHint:           "Good fit if you want pay-as-you-go access to open models without running your own infrastructure.",
		DocsURL:             "https://deepinfra.com/",
		SignupURL:           "https://deepinfra.com/dash/api_keys",
		APIKeyLabel:         "DeepInfra API Key",
		APIKeyHelp:          "Create a DeepInfra API key, then pick one of the available coding-capable open models.",
		Recommended:         true,
		RecommendedPrefixes: []string{"deepseek-ai/deepseek-v4", "deepseek-ai/deepseek-v3", "qwen/", "zai-org/glm-5", "meta-llama/"},
		RecommendedModelWhy: "Prefer current open coding or reasoning models with good tool-use support.",
	},
	"chutes": {
		Description:         "Low-friction hosted inference focused on open models and flexible serverless deployment.",
		SetupHint:           "Useful if you want a simple hosted provider for public open models and fast experimentation.",
		DocsURL:             "https://chutes.ai/",
		SignupURL:           "https://chutes.ai/",
		APIKeyLabel:         "Chutes API Key",
		APIKeyHelp:          "Create a Chutes account and API key, then choose a coding-capable open model from the catalog.",
		Recommended:         true,
		RecommendedPrefixes: []string{"qwen/", "deepseek", "glm", "llama"},
		RecommendedModelWhy: "Prefer strong open coding models over older generic chat defaults.",
	},
	"cerebras": {
		Description:         "High-performance provider with fast inference and GLM model support.",
		SetupHint:           "Create a Cerebras API key and start with zai-glm-4.7.",
		DocsURL:             "https://inference-docs.cerebras.ai/",
		SignupURL:           "https://cloud.cerebras.ai/",
		APIKeyLabel:         "Cerebras API Key",
		APIKeyHelp:          "Cerebras offers high-performance inference with fast token generation.",
		Recommended:         false,
		RecommendedPrefixes: []string{},
		RecommendedModelWhy: "Good default for high-performance inference use.",
	},
	"sprout-local": {
		Description:         "Run fully offline on your Mac — no API key, no network. GPU-accelerated via Apple MLX with quantized Qwen3.5 models.",
		SetupHint:           "No setup needed if a model is already downloaded. First use downloads a ~2–5 GB model automatically.",
		DocsURL:             "",
		SignupURL:           "",
		APIKeyLabel:         "",
		APIKeyHelp:          "",
		Recommended:         false,
		RecommendedPrefixes: []string{"qwen3.5-"},
		RecommendedModelWhy: "Best speed-to-capability ratio for local hardware.",
	},
}

func applyOnboardingPresentation(entry onboardingProvider) onboardingProvider {
	if provider, ok := providercatalog.FindProvider(entry.ID); ok {
		entry.Recommended = provider.Recommended
		entry.Description = provider.Description
		entry.SetupHint = provider.SetupHint
		entry.DocsURL = provider.DocsURL
		entry.SignupURL = provider.SignupURL
		entry.APIKeyLabel = provider.APIKeyLabel
		entry.APIKeyHelp = provider.APIKeyHelp
		if provider.RecommendedModel != "" {
			entry.RecommendedModel = provider.RecommendedModel
		}
		if provider.RecommendedModelWhy != "" {
			entry.RecommendedModelWhy = provider.RecommendedModelWhy
		}
		if len(entry.Models) == 0 && len(provider.Models) > 0 {
			entry.Models = make([]string, 0, len(provider.Models))
			for _, model := range provider.Models {
				if strings.TrimSpace(model.ID) == "" {
					continue
				}
				entry.Models = append(entry.Models, model.ID)
			}
		}
		if entry.RecommendedModel == "" {
			entry.RecommendedModel = provider.DefaultModel
		}
	}

	// Probe-first: the capability probe is the authoritative signal for whether
	// a model is usable for primary or subagent work; if the published registry
	// carries probe-backed recommendations for this provider, prefer the
	// strongest one over both the curated catalog entry and the prefix-match
	// fallback below. A short timeout keeps onboarding responsive if the
	// registry is slow or unreachable; any error / no-data falls through to
	// the existing logic. Priority: probe > catalog curated > prefix-match.
	if probe := probeRecommendedModel(entry.ID); probe != "" {
		entry.RecommendedModel = probe
		if entry.RecommendedModelWhy == "" {
			entry.RecommendedModelWhy = "Picked the strongest model confirmed by automated capability testing."
		}
	}

	presentation, ok := onboardingProviderPresentations[entry.ID]
	if !ok {
		return entry
	}

	// The checked-in provider catalog is the primary source of onboarding metadata.
	// Keep these hardcoded values only as fallback defaults for providers whose
	// catalog entries are missing fields during development or refresh failures.
	if entry.Description == "" {
		entry.Description = presentation.Description
	}
	if entry.SetupHint == "" {
		entry.SetupHint = presentation.SetupHint
	}
	if entry.DocsURL == "" {
		entry.DocsURL = presentation.DocsURL
	}
	if entry.SignupURL == "" {
		entry.SignupURL = presentation.SignupURL
	}
	if entry.APIKeyLabel == "" {
		entry.APIKeyLabel = presentation.APIKeyLabel
	}
	if entry.APIKeyHelp == "" {
		entry.APIKeyHelp = presentation.APIKeyHelp
	}
	if !entry.Recommended {
		entry.Recommended = presentation.Recommended
	}
	if entry.RecommendedModel == "" {
		entry.RecommendedModel = resolveRecommendedModel(entry.Models, presentation.RecommendedPrefixes)
	}
	if entry.RecommendedModelWhy == "" {
		entry.RecommendedModelWhy = presentation.RecommendedModelWhy
	}
	return entry
}
