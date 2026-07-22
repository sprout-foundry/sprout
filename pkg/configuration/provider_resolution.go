package configuration

import (
	"fmt"
	"strings"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// displayNameToClientType is the reverse lookup from GetProviderName output
// → ClientType, built lazily on first use. Populated from api.BuiltInClientTypes()
// + any registered custom providers in the supplied Config. The map keys are
// stored lowercased because user input (and persisted session data) may have
// arbitrary casing — e.g. "Ollama (Local)", "ollama (local)", "OLLMAMA (Local)"
// all need to map to OllamaLocalClientType.
//
// Lazily computed (sync.OnceValue-style) because:
//   - Building the map touches every built-in ClientType and looks up its
//     display name, which is cheap but pointless to do at process startup.
//   - MapProviderStringToClientType is on the hot path for chat-session
//     restore and config validation, so we want a single allocation.
//
// See SP-034-fix provider/model mapping round-trip: prior to the fix,
// chat sessions stored display names (via api.GetProviderName) which
// couldn't round-trip back through MapProviderStringToClientType.
var displayNameToClientTypeOnce sync.Once
var displayNameToClientType map[string]api.ClientType

func buildDisplayNameToClientType() map[string]api.ClientType {
	m := make(map[string]api.ClientType, len(api.BuiltInClientTypes()))
	// Iterate the canonical (non-alias) ClientTypes only. The "ollama"
	// ClientType is an alias for ollama-local (see api.ParseProviderName);
	// both GetProviderName("ollama") and GetProviderName("ollama-local")
	// return "Ollama (Local)". If we registered both, the second write
	// would either silently overwrite the first or get blocked by our
	// ambiguity guard. The alias is already handled by the primary ID
	// switch ("ollama" → OllamaClientType), so omitting it here is safe.
	canonical := make([]api.ClientType, 0, len(api.BuiltInClientTypes()))
	for _, ct := range api.BuiltInClientTypes() {
		if ct == api.OllamaClientType {
			continue
		}
		canonical = append(canonical, ct)
	}
	for _, ct := range canonical {
		name := strings.TrimSpace(api.GetProviderName(ct))
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		// Don't let two ClientTypes share a lowercase display name — last
		// write wins, but we'd rather surface the conflict as ambiguous
		// than silently pick wrong. (Today all canonical built-ins have
		// unique display names so this branch is dead, but the guard
		// keeps it that way if someone adds an alias in the future.)
		if existing, ok := m[key]; ok && existing != ct {
			continue
		}
		m[key] = ct
	}
	return m
}

// lookupClientTypeByDisplayName returns the ClientType whose display name
// (from api.GetProviderName) matches the lowercased trimmed raw string.
// Returns ("", false) when no match is found.
func lookupClientTypeByDisplayName(raw string) (api.ClientType, bool) {
	displayNameToClientTypeOnce.Do(func() {
		displayNameToClientType = buildDisplayNameToClientType()
	})
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return "", false
	}
	ct, ok := displayNameToClientType[key]
	return ct, ok
}

// MapProviderStringToClientType converts a provider string to ClientType, including
// built-in providers, custom providers from config, and factory-backed dynamic providers.
//
// Inputs accepted in priority order:
//  1. Built-in ClientType IDs ("openai", "ollama-local", "zai-coding", ...)
//  2. Custom providers from cfg.CustomProviders
//  3. Factory-loaded embedded provider configs
//  4. Display names ("OpenAI", "Ollama (Local)", ...) — reverse lookup
//     against api.GetProviderName output. Provided for backward compatibility
//     with sessions that persisted display names before the SP-034-fix.
func MapProviderStringToClientType(cfg *Config, raw string) (api.ClientType, error) {
	name := strings.TrimSpace(strings.ToLower(raw))
	switch name {
	case "openai":
		return api.OpenAIClientType, nil
	case "chutes":
		return api.ChutesClientType, nil
	case "zai":
		return api.ZAIClientType, nil
	case "openrouter":
		return api.OpenRouterClientType, nil
	case "deepinfra":
		return api.DeepInfraClientType, nil
	case "deepseek":
		return api.DeepSeekClientType, nil
	case "ollama":
		return api.OllamaClientType, nil
	case "ollama-local":
		return api.OllamaLocalClientType, nil
	case "ollama-cloud":
		return api.OllamaCloudClientType, nil
	case "lmstudio":
		return api.LMStudioClientType, nil
	case "mistral":
		return api.MistralClientType, nil
	case "minimax":
		return api.MinimaxClientType, nil
	case "test":
		return api.TestClientType, nil
	case "editor":
		return api.EditorClientType, nil
	}

	if cfg != nil && cfg.CustomProviders != nil {
		if _, exists := cfg.CustomProviders[name]; exists {
			return api.ClientType(name), nil
		}
	}

	providerFactory := providers.NewProviderFactory()
	if err := providerFactory.LoadEmbeddedConfigs(); err == nil {
		if _, err := providerFactory.GetProviderConfig(name); err == nil {
			return api.ClientType(name), nil
		}
	}

	// Backward compatibility: if none of the ID-based lookups matched, the
	// caller may have handed us a display name (e.g. "Ollama (Local)" from
	// a session persisted before the SP-034-fix). Look up the ClientType
	// whose GetProviderName output matches.
	if ct, ok := lookupClientTypeByDisplayName(raw); ok {
		return ct, nil
	}

	return "", fmt.Errorf("unsupported provider: %s", raw)
}

// ResolveProviderModel resolves provider and model using one canonical precedence path:
// 1) Explicit provider flag/arg
// 2) Explicit model in provider:model format (only when prefix is a valid provider)
// 3) SPROUT_PROVIDER env (with LEDIT_PROVIDER backward-compat)
// 4) SPROUT_MODEL env (provider:model format only when prefix is a valid provider, LEDIT_MODEL backward-compat)
// 5) Config last_used_provider
// 6) Auto-detected provider via DetermineProvider
//
// Model precedence:
// 1) Explicit model (trimmed to model segment when provider:model format is recognized)
// 2) SPROUT_MODEL env (same parsing rule, LEDIT_MODEL backward-compat)
// 3) Config provider model default
func ResolveProviderModel(cfg *Config, explicitProvider, explicitModel string) (api.ClientType, string, error) {
	providerName := strings.TrimSpace(explicitProvider)
	modelCandidate := strings.TrimSpace(explicitModel)

	if providerName == "" && modelCandidate != "" {
		if parsedProvider, parsedModel, ok := parseProviderModelSpecifier(cfg, modelCandidate); ok {
			providerName = parsedProvider
			modelCandidate = parsedModel
		}
	}

	if providerName == "" {
		providerName = strings.TrimSpace(GetEnvSimple("PROVIDER"))
	}
	if modelCandidate == "" {
		modelCandidate = strings.TrimSpace(GetEnvSimple("MODEL"))
	}

	if providerName == "" && modelCandidate != "" {
		if parsedProvider, parsedModel, ok := parseProviderModelSpecifier(cfg, modelCandidate); ok {
			providerName = parsedProvider
			modelCandidate = parsedModel
		}
	}

	if providerName == "" && cfg != nil {
		providerName = strings.TrimSpace(cfg.LastUsedProvider)
	}

	// Never use the test provider from persisted config — it's only for
	// process-scoped testing (isRunningUnderTest) and must not leak into
	// real sessions. The explicitProvider == "test" path (from parent
	// agent's GetProvider() during test) is allowed through so subagent
	// creation works under test without the now-removed implicit fallbacks.
	if providerName == "test" && explicitProvider != "test" {
		providerName = ""
	}

	var clientType api.ClientType
	var err error
	if providerName != "" {
		clientType, err = MapProviderStringToClientType(cfg, providerName)
		if err != nil {
			return "", "", fmt.Errorf("map provider string to client type: %w", err)
		}
	} else {
		// Pass providerName (from env or config) as lastUsedProvider to enable
		// proper fallback in DetermineProvider before auto-detection
		clientType, err = api.DetermineProvider("", api.ClientType(providerName))
		if err != nil {
			return "", "", fmt.Errorf("failed to determine provider: %w", err)
		}
	}

	if modelCandidate == "" && cfg != nil {
		modelCandidate = strings.TrimSpace(cfg.GetModelForProvider(string(clientType)))
	}

	return clientType, modelCandidate, nil
}

func parseProviderModelSpecifier(cfg *Config, raw string) (string, string, bool) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	prefix := strings.TrimSpace(parts[0])
	model := strings.TrimSpace(parts[1])
	if prefix == "" || model == "" {
		return "", "", false
	}
	if _, err := MapProviderStringToClientType(cfg, prefix); err != nil {
		return "", "", false
	}
	return prefix, model, true
}
