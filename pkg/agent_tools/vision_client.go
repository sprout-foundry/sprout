package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// testVisionCapabilityOverride pins HasVisionCapability's verdict for tests
// (nil = use the real environment probe). See SetVisionCapabilityForTest.
var testVisionCapabilityOverride atomic.Pointer[bool]

// ============================================================================
// Vision Processor Constructors
// ============================================================================

// NewVisionProcessor creates a vision processor with the given client
func NewVisionProcessor(client api.ClientInterface, logger *utils.Logger, debug bool) *VisionProcessor {
	return &VisionProcessor{
		visionClient: client,
		logger:       logger,
		debug:        debug,
	}
}

// ============================================================================
// Custom Provider Configuration Helpers
// ============================================================================

// GetCustomProviderConfig returns the custom provider configuration for a given type
func GetCustomProviderConfig(providerType api.ClientType) (configuration.CustomProviderConfig, bool) {
	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		return configuration.CustomProviderConfig{}, false
	}
	config := configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return configuration.CustomProviderConfig{}, false
	}

	customConfig, exists := config.CustomProviders[string(providerType)]
	if !exists {
		return configuration.CustomProviderConfig{}, false
	}
	return customConfig, true
}

// GetCustomVisionProviders returns a list of custom providers that support vision
func GetCustomVisionProviders() []api.ClientType {
	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		return nil
	}
	config := configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return nil
	}

	providers := make([]api.ClientType, 0, len(config.CustomProviders))
	for name, custom := range config.CustomProviders {
		// Tri-state: only an explicit opt-out excludes a provider.
		if custom.SupportsVision != nil && !*custom.SupportsVision {
			continue
		}
		providers = append(providers, api.ClientType(name))
	}
	return providers
}

// GetCustomVisionFallback returns the fallback provider and model for vision
func GetCustomVisionFallback(providerType api.ClientType) (api.ClientType, string, bool) {
	customConfig, ok := GetCustomProviderConfig(providerType)
	if !ok {
		return "", "", false
	}

	fallbackProvider := strings.TrimSpace(customConfig.VisionFallbackProvider)
	if fallbackProvider == "" {
		return "", "", false
	}

	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		return "", "", false
	}

	fallbackClientType, err := configManager.MapStringToClientType(fallbackProvider)
	if err != nil {
		return "", "", false
	}

	return fallbackClientType, strings.TrimSpace(customConfig.VisionFallbackModel), true
}

// createOCRClient creates a client for the configured OCR fallback model
// (ocr_fallback_model, "provider/model" form) via the normal factory.
// Provider-agnostic: any provider the factory can build works.
func createOCRClient(model string) (api.ClientInterface, error) {
	providerType, modelName := splitProviderQualifiedModel(model)
	if providerType == "" || modelName == "" {
		return nil, fmt.Errorf("invalid OCR fallback model %q: expected provider/model", model)
	}
	client, err := factory.CreateProviderClient(api.ClientType(providerType), modelName)
	if err != nil {
		return nil, fmt.Errorf("create OCR fallback client for %s: %w", model, err)
	}
	return client, nil
}

// splitProviderQualifiedModel splits a "provider/model" string. When there
// is no slash, the model alone is returned with an empty provider.
func splitProviderQualifiedModel(model string) (provider, modelName string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", ""
	}
	if idx := strings.Index(model, "/"); idx > 0 {
		return model[:idx], model[idx+1:]
	}
	return "", model
}

// ============================================================================
// Vision Processor Creation
// ============================================================================

// NewVisionProcessorWithMode creates a vision processor for image/OCR
// workflows, scoped to providerType. An empty providerType yields a processor
// with no vision client, so callers report "vision unavailable" rather than
// borrowing an unrelated provider's model.
func NewVisionProcessorWithMode(debug bool, providerType api.ClientType) (*VisionProcessor, error) {
	if providerType == "" {
		return &VisionProcessor{logger: nil, debug: debug}, nil
	}
	return NewVisionProcessorWithProvider(debug, providerType)
}

// NewVisionProcessorWithProvider creates a vision processor using the specified provider
func NewVisionProcessorWithProvider(debug bool, providerType api.ClientType) (*VisionProcessor, error) {
	client, err := CreateVisionClientWithProvider(providerType)
	if err != nil {
		return nil, fmt.Errorf("create vision client for provider %s: %w", providerType, err)
	}

	return &VisionProcessor{
		visionClient: client,
		logger:       nil,
		debug:        debug,
	}, nil
}

// CreateVisionClientWithProvider creates a vision client for one provider.
//
// Resolution stays inside that provider: its active model, then its explicit
// vision_model pin, then its configured vision_fallback_provider. There is no
// global walk — a turn on provider A must never be served by provider B's
// model, which is how an unrelated (and possibly retired) vision_model pin
// used to surface in a session that never configured it.
func CreateVisionClientWithProvider(providerType api.ClientType) (api.ClientInterface, error) {
	if providerType == "" || providerType == api.TestClientType {
		return nil, fmt.Errorf("no vision provider named")
	}
	// A provider with no credentials cannot build a client; the local
	// runtimes need none.
	if !configuration.HasProviderAuth(string(providerType)) && !isLocalRuntimeProvider(providerType) {
		return nil, fmt.Errorf("provider %s has no credentials configured", providerType)
	}

	var retired []string

	tryModel := func(provider api.ClientType, model string) api.ClientInterface {
		client, err := factory.CreateProviderClient(provider, model)
		if err != nil || !client.SupportsVision() {
			return nil
		}
		// Declared support is config; a retired model keeps its vision tag.
		// Rejecting it here (not only in the capability probe) keeps "this
		// provider has vision" and "this provider yields a vision client"
		// from disagreeing.
		if !visionModelServed(context.Background(), client, model) {
			retired = append(retired, fmt.Sprintf("%s/%s", provider, model))
			return nil
		}
		return client
	}

	// Get the vision model for this provider
	if visionModel := GetVisionModelForProvider(providerType); visionModel != "" {
		if client := tryModel(providerType, visionModel); client != nil {
			return client, nil
		}
	}

	// A custom provider may name an explicit fallback provider/model. This is
	// the provider's OWN declared fallback, not a search of other providers.
	fallbackProvider, fallbackModel, hasFallback := GetCustomVisionFallback(providerType)
	if hasFallback {
		if fallbackModel == "" {
			fallbackModel = GetVisionModelForProvider(fallbackProvider)
		}
		if fallbackModel != "" {
			if client := tryModel(fallbackProvider, fallbackModel); client != nil {
				return client, nil
			}
		}
	}

	if len(retired) > 0 {
		// Name the dead pin: a config change is the fix, and this is the only
		// place the operator sees which model was tried.
		return nil, fmt.Errorf("provider %s has no usable vision model: the configured vision model is no longer served by its provider (%s)",
			providerType, strings.Join(retired, ", "))
	}
	return nil, fmt.Errorf("provider %s does not support vision models and no fallback is configured for it", providerType)
}

// GetVisionModelForProvider returns the appropriate vision model for a given
// provider, preferring the ACTIVE model when it is itself vision-capable.
//
// Resolution order (SP-137: runtime > declared, provider-neutral):
//  1. The provider's active (default) model, when its model_info entry is
//     tagged vision-capable — a natively-multimodal active model must not
//     be forked away from. (Observed failure this fixes: an agent running
//     glm-5.3-flash had every vision call forked to the registry-pinned
//     glm-5v-turbo, which the user's plan did not include.)
//  2. Custom providers' explicit vision_model / model_name config.
//  3. The registry vision_model pin (pkg/agent_providers/configs/*.json).
//  4. The provider's default model via a client's GetVisionModel().
func GetVisionModelForProvider(providerType api.ClientType) string {
	switch providerType {
	case api.TestClientType:
		return ""
	}

	// The active model first: when it is itself vision-capable, the vision
	// tier IS the active model. The declared-capability read mirrors
	// SupportsVision's model_info tier (tags over provider flag).
	if cfg, err := factory.GlobalFactory().GetProviderConfig(string(providerType)); err == nil && cfg != nil {
		if active := strings.TrimSpace(cfg.Defaults.Model); active != "" && providerModelSeesImages(cfg, active) {
			return active
		}
	}

	// Check custom provider config first for explicit vision settings.
	if customConfig, ok := GetCustomProviderConfig(providerType); ok {
		if customConfig.SupportsVision != nil && !*customConfig.SupportsVision {
			return ""
		}
		if strings.TrimSpace(customConfig.VisionModel) != "" {
			return strings.TrimSpace(customConfig.VisionModel)
		}
		return strings.TrimSpace(customConfig.ModelName)
	}

	// Registry config: an explicit vision_model beats everything below and
	// works even when defaults.model is unset (SP-137: config-driven, no
	// throwaway client needed for the common case).
	if cfg, err := factory.GlobalFactory().GetProviderConfig(string(providerType)); err == nil && cfg != nil {
		if vm := strings.TrimSpace(cfg.Models.VisionModel); vm != "" {
			return vm
		}
	}

	// No explicit vision model: the provider's default model must itself
	// be vision-capable, resolved through a client's GetVisionModel.
	model := GetDefaultModelForProvider(providerType)
	if model == "" {
		return ""
	}

	client, err := factory.CreateProviderClient(providerType, model)
	if err != nil {
		return ""
	}

	// Get vision model from the provider
	return client.GetVisionModel()
}

// providerModelSeesImages reports whether one model of a provider config is
// vision-capable — the same declared-capability read the live client's
// SupportsVision performs, applied to the config's model table so the check
// needs no client construction. Mirrors its semantics exactly: an explicit
// provider-wide opt-out vetoes everything; a model_info entry that names a
// modality is authoritative; otherwise vision is assumed.
func providerModelSeesImages(cfg *providers.ProviderConfig, model string) bool {
	if cfg == nil || !cfg.Models.SeesImages() {
		return false
	}
	if info := cfg.GetModelInfo(model); info != nil {
		if verdict, known := providers.ModelInfoVisionVerdict(*info); known {
			return verdict
		}
	}
	return true
}

// GetDefaultModelForProvider returns the provider's configured default
// model from its registry config (SP-137: no per-provider model names in
// the vision tier). Empty when the provider or default is unknown.
func GetDefaultModelForProvider(providerType api.ClientType) string {
	cfg, err := factory.GlobalFactory().GetProviderConfig(string(providerType))
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Defaults.Model)
}

// CreateVisionClient creates a client capable of vision analysis for one
// provider. There is no global walk: the caller must name the provider it is
// running on (see CreateVisionClientWithProvider).
func CreateVisionClient(providerType api.ClientType) (api.ClientInterface, error) {
	return CreateVisionClientWithProvider(providerType)
}

// isLocalRuntimeProvider reports whether the provider needs no credentials
// (a local model runtime). Derived from the provider config's auth type —
// "none" means no key is ever required — plus the built-in local client
// types that have no config file.
func isLocalRuntimeProvider(providerType api.ClientType) bool {
	// Built-in local client types have no provider config file.
	switch providerType {
	case api.OllamaClientType, api.OllamaLocalClientType, api.LMStudioClientType:
		return true
	}
	// Registry-driven: configs declaring auth.type "none" never need keys.
	cfg, err := factory.GlobalFactory().GetProviderConfig(string(providerType))
	if err != nil || cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Auth.Type), "none")
}

// SetVisionCapabilityForTest pins HasVisionCapability's verdict for the
// duration of a test and returns a restore func the caller should defer (or
// register with t.Cleanup). It exists so a test can exercise "no vision tier
// available" deterministically: the real probe depends on host state (compiled
// native-OCR shims, configured providers), which differs between a developer
// machine and CI.
//
// Test-only seam: production code must never call it.
func SetVisionCapabilityForTest(available bool) func() {
	testVisionCapabilityOverride.Store(&available)
	return func() { testVisionCapabilityOverride.Store(nil) }
}

// ResetVisionCapabilityCacheForTest clears the capability cache, so a test
// that changes the environment (keys, config, native-OCR availability) can
// re-probe instead of reading a verdict cached earlier in the process.
func ResetVisionCapabilityCacheForTest() {
	visionCapabilityMu.Lock()
	defer visionCapabilityMu.Unlock()
	visionCapabilityValues = nil
}

// visionCapabilityValues caches one probe verdict per provider for the
// process, so a run does not re-resolve provider clients per call. Tests can
// pin the verdict with SetVisionCapabilityForTest (which bypasses the cache)
// or clear it with ResetVisionCapabilityCacheForTest.
var (
	visionCapabilityMu     sync.Mutex
	visionCapabilityValues map[api.ClientType]bool
)

// HasVisionCapability reports whether vision processing is available at all,
// without naming a provider: native OCR, or any configured provider that can
// build a vision-capable client. Use HasVisionCapabilityFor when the caller
// knows which provider the turn runs on.
func HasVisionCapability() bool {
	return HasVisionCapabilityFor("")
}

// HasVisionCapabilityFor reports whether vision is available to a turn
// running on providerType. Native OCR counts regardless of provider. When
// providerType is empty, any configured provider may answer.
//
// The verdict is cached per provider for the process: the probe constructs
// clients and may cost one model listing (vision_model_probe.go), which must
// not be repaid per turn.
func HasVisionCapabilityFor(providerType api.ClientType) bool {
	if override := testVisionCapabilityOverride.Load(); override != nil {
		return *override
	}

	visionCapabilityMu.Lock()
	defer visionCapabilityMu.Unlock()
	if visionCapabilityValues == nil {
		visionCapabilityValues = map[api.ClientType]bool{}
	}
	if v, ok := visionCapabilityValues[providerType]; ok {
		return v
	}
	v := probeVisionCapability(providerType)
	visionCapabilityValues[providerType] = v
	return v
}

// probeVisionCapability resolves vision availability for one provider: that
// provider's own vision client, or native OCR when the caller named no
// provider (native OCR is provider-independent, so it answers the
// provider-agnostic question but NOT "can this provider see").
func probeVisionCapability(providerType api.ClientType) bool {
	if providerType == "" {
		// Provider-agnostic: native OCR counts, since text extraction needs
		// no provider at all (SP-137).
		if nativeOCRAvailable() {
			return true
		}
		// Any configured provider may answer. The list is sorted so the
		// answer is deterministic — GetAvailableProviders iterates a map and
		// would otherwise vary call to call.
		for _, pt := range sortedVisionProviders() {
			if providerHasVision(pt) {
				return true
			}
		}
		return false
	}

	// Provider-scoped: the answer is whether THIS provider yields a vision
	// client. Native OCR is not a substitute — it extracts text, it does not
	// let the provider see.
	return providerHasVision(providerType)
}

// providerHasVision reports whether one provider can build a vision-capable
// client whose model the provider still serves. It delegates to the same
// constructor the callers use, so the capability answer and the client
// actually obtained cannot disagree.
func providerHasVision(providerType api.ClientType) bool {
	client, err := CreateVisionClientWithProvider(providerType)
	return err == nil && client != nil
}

// sortedVisionProviders returns every known provider in a deterministic
// order. GetAvailableProviders iterates a map, so callers that need a stable
// answer must sort.
func sortedVisionProviders() []api.ClientType {
	names := factory.GlobalFactory().GetAvailableProviders()
	out := make([]api.ClientType, 0, len(names))
	for _, n := range names {
		out = append(out, api.ClientType(n))
	}
	out = append(out, GetCustomVisionProviders()...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
