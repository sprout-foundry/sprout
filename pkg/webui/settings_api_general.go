//go:build !js

package webui

import (
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	agentpkg "github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// ---------------------------------------------------------------------------
// Method router — General settings
// ---------------------------------------------------------------------------

// handleAPISettings dispatches GET and PUT /api/settings.
func (ws *ReactWebServer) handleAPISettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsPut(w, r)
	default:
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

// enrichCustomProviders loads custom provider files from the providers
// directories into cfg. Config.json never stores custom providers (they are
// set to nil before saving), so raw-file reads always miss them. This
// helper bridges the gap: it reads the global home directory AND the
// scoped directory, with the scoped directory winning on name conflicts —
// providers are always saved globally, so a settings view under an isolated
// config still has to see them.
func enrichCustomProviders(cfg *configuration.Config) {
	if cfg == nil {
		return
	}
	if cfg.CustomProviders == nil {
		cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
	}
	merge := func(dir string) {
		// A partial read (some files skipped) still contributes its valid
		// providers; the error names the skipped files.
		fileProviders, err := configuration.LoadCustomProvidersFromDir(dir)
		if err != nil {
			slog.Default().Warn("custom provider files partially loaded", slog.Any("err", err))
		}
		for name, provider := range fileProviders {
			cfg.CustomProviders[name] = provider
		}
	}
	if globalDir, err := configuration.DefaultConfigDir(); err == nil {
		merge(filepath.Join(globalDir, configuration.ProvidersDirName))
	}
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		slog.Default().Warn("failed to resolve config directory", slog.Any("err", err))
		return
	}
	merge(filepath.Join(configDir, configuration.ProvidersDirName))
}

// applySystemPromptToLiveAgents distributes a system prompt update to all
// live agents that are currently using the base/orchestrator persona.
func (ws *ReactWebServer) applySystemPromptToLiveAgents(systemPrompt string) {
	if strings.TrimSpace(systemPrompt) == "" {
		return
	}

	ws.mutex.RLock()
	agents := make([]*agentpkg.Agent, 0, len(ws.clientContexts))
	seen := make(map[*agentpkg.Agent]struct{})
	for _, ctx := range ws.clientContexts {
		if ctx == nil || ctx.Agent == nil {
			continue
		}
		if _, exists := seen[ctx.Agent]; exists {
			continue
		}
		agents = append(agents, ctx.Agent)
		seen[ctx.Agent] = struct{}{}
	}
	ws.mutex.RUnlock()

	for _, agentInst := range agents {
		agentInst.SetBaseSystemPrompt(systemPrompt)
		activePersona := strings.TrimSpace(agentInst.GetActivePersona())
		if activePersona == "" || activePersona == personas.IDOrchestrator {
			agentInst.SetSystemPrompt(systemPrompt)
		}
	}
}

// refreshContextCapOnLiveAgents re-resolves the effective context cap from
// config on every live agent after a runtime MaxContextTokens change, so the
// running session honors the new cap without waiting for a model switch.
func (ws *ReactWebServer) refreshContextCapOnLiveAgents() {
	ws.mutex.RLock()
	agents := make([]*agentpkg.Agent, 0, len(ws.clientContexts))
	seen := make(map[*agentpkg.Agent]struct{})
	for _, ctx := range ws.clientContexts {
		if ctx == nil {
			continue
		}
		if ctx.Agent != nil {
			if _, exists := seen[ctx.Agent]; !exists {
				agents = append(agents, ctx.Agent)
				seen[ctx.Agent] = struct{}{}
			}
		}
		for _, cs := range ctx.ChatSessions {
			if cs == nil {
				continue
			}
			cs.mu.RLock()
			agentInst := cs.Agent
			cs.mu.RUnlock()
			if agentInst != nil {
				if _, exists := seen[agentInst]; !exists {
					agents = append(agents, agentInst)
					seen[agentInst] = struct{}{}
				}
			}
		}
	}
	ws.mutex.RUnlock()

	for _, agentInst := range agents {
		agentInst.RefreshContextCapFromConfig()
	}
}

// expandNestedKeys recursively expands nested maps in a flat dot-path map.
// e.g. {"a": {"b": 1}} becomes {"a.b": 1, "a": {"b": 1}}
// The original keys are preserved alongside the expanded ones.
func expandNestedKeys(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return make(map[string]interface{})
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
		if sub, ok := v.(map[string]interface{}); ok {
			for sk, sv := range expandNestedKeys(sub) {
				result[k+"."+sk] = sv
			}
		}
	}
	return result
}
