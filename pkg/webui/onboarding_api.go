package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/providercatalog"
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
		Description:         "Good first choice for coding-focused use. Z.AI also has a dedicated coding plan and remote MCP services.",
		SetupHint:           "Use either a standard Z.AI API key or, if you already have one, a GLM Coding Plan setup.",
		DocsURL:             "https://docs.z.ai/devpack/overview",
		SignupURL:           "https://platform.z.ai/",
		APIKeyLabel:         "Z.AI API Key",
		APIKeyHelp:          "Create a key in the Z.AI API platform. Coding Plan subscriptions are separate from normal API billing.",
		Recommended:         true,
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
		Recommended:         true,
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
		RecommendedPrefixes: []string{"deepseek-ai/deepseek-v3", "qwen/", "zai-org/glm-5", "meta-llama/"},
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
}

var onboardingProviderOrder = map[string]int{
	"zai":        0,
	"minimax":    1,
	"openrouter": 2,
	"deepinfra":  3,
	"chutes":     4,
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

func resolveRecommendedModel(models []string, prefixes []string) string {
	for _, prefix := range prefixes {
		for _, model := range models {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), strings.ToLower(prefix)) {
				return model
			}
		}
	}
	if len(models) > 0 {
		return strings.TrimSpace(models[0])
	}
	return ""
}

func detectOnboardingEnvironment() onboardingEnvironment {
	hostPlatform := strings.TrimSpace(os.Getenv("LEDIT_HOST_PLATFORM"))
	if hostPlatform == "" {
		hostPlatform = runtime.GOOS
	}

	backendMode := strings.TrimSpace(os.Getenv("LEDIT_DESKTOP_BACKEND_MODE"))
	if backendMode == "" {
		backendMode = "native"
	}

	hasWSL := backendMode == "wsl"
	if !hasWSL && hostPlatform == "windows" {
		_, err := exec.LookPath("wsl.exe")
		hasWSL = err == nil
	}

	hasGitBash := hasGitBashShell()
	recommendedTerminal := "system"
	if hostPlatform == "windows" {
		if backendMode == "wsl" || hasWSL {
			recommendedTerminal = "wsl"
		} else if hasGitBash {
			recommendedTerminal = "git-bash"
		}
	}

	// Detect active WSL distro (set by the WSL runtime environment).
	activeDistro := strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME"))

	// Enumerate available WSL distros when on Windows native or when wsl.exe is reachable.
	wslDistros := detectWslDistros(hasWSL, backendMode, hostPlatform)

	return onboardingEnvironment{
		RuntimePlatform:     runtime.GOOS,
		HostPlatform:        hostPlatform,
		BackendMode:         backendMode,
		HasWSL:              hasWSL,
		HasGitBash:          hasGitBash,
		RecommendedTerminal: recommendedTerminal,
		ActiveDistro:        activeDistro,
		WslDistros:          wslDistros,
	}
}

func detectWslDistros(hasWSL bool, backendMode, hostPlatform string) []string {
	// Only attempt on Windows native (not from inside WSL) or when wsl.exe is known to be present.
	if !hasWSL {
		return nil
	}
	if hostPlatform != "windows" && backendMode != "wsl" {
		return nil
	}
	cmd := exec.Command("wsl.exe", "-l", "-q")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var distros []string
	for _, line := range strings.Split(string(out), "\n") {
		// wsl.exe -l -q uses UTF-16LE on Windows; when running inside WSL it emits UTF-8.
		// Strip BOM and null bytes that appear in the UTF-16 output.
		clean := strings.Map(func(r rune) rune {
			if r == 0 || r == '\r' || r == '\ufeff' {
				return -1
			}
			return r
		}, line)
		clean = strings.TrimSpace(clean)
		// Strip the leading '*' that wsl.exe uses to mark the default distro.
		clean = strings.TrimLeft(clean, "* ")
		clean = strings.TrimSpace(clean)
		if clean != "" {
			distros = append(distros, clean)
		}
	}
	return distros
}

func hasGitBashShell() bool {
	if _, err := exec.LookPath("bash"); err == nil {
		return true
	}

	programFiles := []string{
		strings.TrimSpace(os.Getenv("ProgramFiles")),
		strings.TrimSpace(os.Getenv("ProgramW6432")),
		strings.TrimSpace(os.Getenv("ProgramFiles(x86)")),
	}

	for _, root := range programFiles {
		if root == "" {
			continue
		}
		candidates := []string{
			filepath.Join(root, "Git", "bin", "bash.exe"),
			filepath.Join(root, "Git", "usr", "bin", "bash.exe"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return true
			}
		}
	}

	return false
}

func (ws *ReactWebServer) handleAPIOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	descriptors := ws.listProviders(ws.resolveClientID(r))
	providers := make([]onboardingProvider, 0, len(descriptors))
	indexByID := make(map[string]onboardingProvider, len(descriptors))

	for _, desc := range descriptors {
		meta, _ := configuration.GetProviderAuthMetadata(desc.ID)
		hasCredential := configuration.HasProviderAuth(desc.ID)
		entry := onboardingProvider{
			ID:             desc.ID,
			Name:           desc.Name,
			Models:         desc.Models,
			RequiresAPIKey: meta.RequiresAPIKey,
			HasCredential:  hasCredential,
		}
		entry = applyOnboardingPresentation(entry)
		providers = append(providers, entry)
		indexByID[entry.ID] = entry
	}

	sort.SliceStable(providers, func(i, j int) bool {
		leftOrder, leftHasOrder := onboardingProviderOrder[providers[i].ID]
		rightOrder, rightHasOrder := onboardingProviderOrder[providers[j].ID]
		switch {
		case leftHasOrder && rightHasOrder:
			return leftOrder < rightOrder
		case leftHasOrder:
			return true
		case rightHasOrder:
			return false
		case providers[i].Recommended != providers[j].Recommended:
			return providers[i].Recommended
		case providers[i].Name == providers[j].Name:
			return providers[i].ID < providers[j].ID
		default:
			return providers[i].Name < providers[j].Name
		}
	})

	currentProvider := strings.TrimSpace(cfg.LastUsedProvider)
	if clientAgent, err := ws.getClientAgent(ws.resolveClientID(r)); err == nil && clientAgent != nil {
		if provider := strings.TrimSpace(clientAgent.GetProvider()); provider != "" && provider != "unknown" && provider != "test" {
			currentProvider = provider
		}
	}
	if currentProvider == "" || currentProvider == "test" {
		for _, provider := range providers {
			if provider.ID == "test" {
				continue
			}
			if !provider.RequiresAPIKey || provider.HasCredential {
				currentProvider = provider.ID
				break
			}
		}
	}
	currentModel := strings.TrimSpace(cfg.GetModelForProvider(currentProvider))
	if clientAgent, err := ws.getClientAgent(ws.resolveClientID(r)); err == nil && clientAgent != nil {
		if model := strings.TrimSpace(clientAgent.GetModel()); model != "" && model != "unknown" {
			currentModel = model
		}
	}
	if currentModel == "" {
		if p, ok := indexByID[currentProvider]; ok {
			if strings.TrimSpace(p.RecommendedModel) != "" {
				currentModel = strings.TrimSpace(p.RecommendedModel)
			} else if len(p.Models) > 0 {
				currentModel = strings.TrimSpace(p.Models[0])
			}
		}
	}

	setupRequired := false
	reason := ""
	if currentProvider == "" || currentProvider == "test" {
		setupRequired = true
		reason = "provider_not_configured"
	} else if p, ok := indexByID[currentProvider]; ok && p.RequiresAPIKey && !p.HasCredential {
		setupRequired = true
		reason = "missing_provider_credential"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"setup_required":   setupRequired,
		"reason":           reason,
		"current_provider": currentProvider,
		"current_model":    currentModel,
		"providers":        providers,
		"environment":      detectOnboardingEnvironment(),
	})
}

func (ws *ReactWebServer) handleAPIOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	clientAgent, err := ws.getClientAgent(clientID)
	if err != nil || clientAgent == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Agent is not available")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req.APIKey = strings.TrimSpace(req.APIKey)
	if req.Provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider is required")
		return
	}

	providerType, err := cm.MapStringToClientType(req.Provider)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	meta, _ := configuration.GetProviderAuthMetadata(req.Provider)
	hasCredential := configuration.HasProviderAuth(req.Provider)

	if meta.RequiresAPIKey && !hasCredential && req.APIKey == "" {
		writeJSONError(w, http.StatusBadRequest, "api_key is required for this provider")
		return
	}

	if req.APIKey != "" {
		keys := cm.GetAPIKeys()
		keys.SetAPIKey(req.Provider, req.APIKey)
		if err := cm.SaveAPIKeys(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save API key: %v", err))
			return
		}
	}

	if err := clientAgent.SetProvider(providerType); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Model != "" {
		if err := clientAgent.SetModel(req.Model); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	_ = ws.syncAgentStateForClient(clientID)
	ws.publishProviderState(clientID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  "Onboarding completed",
		"provider": clientAgent.GetProvider(),
		"model":    clientAgent.GetModel(),
	})
}

func (ws *ReactWebServer) handleAPIOnboardingSkip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	// Set last used provider to "editor" to indicate editor-only mode
	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = "editor"
		return nil
	}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to skip onboarding: %v", err))
		return
	}

	// Sync state and notify the client so the frontend picks up the
	// provider change without requiring a full status poll.
	_ = ws.syncAgentStateForClient(clientID)
	ws.publishProviderState(clientID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": "editor",
		"model":    "",
	})
}
