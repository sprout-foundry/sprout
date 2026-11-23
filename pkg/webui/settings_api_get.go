//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// GET /api/settings
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsGet(w http.ResponseWriter, r *http.Request) {
	layer := strings.TrimSpace(r.URL.Query().Get("layer"))

	switch layer {
	case "global":
		ws.handleGetGlobalSettings(w, r)
		return
	case "workspace":
		ws.handleGetProjectFolderSettings(w, r)
		return
	case "session":
		ws.handleGetSessionSettings(w, r)
		return
	case "provenance":
		ws.handleGetProvenanceSettings(w, r)
		return
	}

	// Default: return effective merged config (current behavior)
	// But avoid blocking if no client context exists
	clientID := ws.resolveClientID(r)

	// Check if client context exists without creating it
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()

	if ctx == nil {
		// No client context exists - return global config instead of blocking
		// trying to create an agent. This fixes the hang when GET /api/settings
		// is called before any client has connected.
		ws.handleGetGlobalSettings(w, r)
		return
	}

	// Client context exists, try to get the config manager
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, sanitizedConfig(cfg))
}

// handleGetGlobalSettings returns the global config file contents.
func (ws *ReactWebServer) handleGetGlobalSettings(w http.ResponseWriter, r *http.Request) {
	configPath, err := configuration.GetConfigPath()
	if err != nil {
		writeJSON(w, http.StatusOK, sanitizedConfig(configuration.NewConfig()))
		return
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		writeJSON(w, http.StatusOK, sanitizedConfig(configuration.NewConfig()))
		return
	}
	var cfg configuration.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		writeJSON(w, http.StatusOK, sanitizedConfig(configuration.NewConfig()))
		return
	}
	enrichCustomProviders(&cfg)
	writeJSON(w, http.StatusOK, sanitizedConfig(&cfg))
}

// handleGetProjectFolderSettings returns the project folder config if it exists, else empty.
func (ws *ReactWebServer) handleGetProjectFolderSettings(w http.ResponseWriter, r *http.Request) {
	projectFolder := ws.getProjectFolderForRequest(r)
	if projectFolder == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	projectFolderPath := configuration.GetProjectFolderConfigPath(projectFolder)
	data, err := os.ReadFile(projectFolderPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	var cfg configuration.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	enrichCustomProviders(&cfg)
	writeJSON(w, http.StatusOK, sanitizedConfig(&cfg))
}

// handleGetSessionSettings returns the current session's config overrides.
func (ws *ReactWebServer) handleGetSessionSettings(w http.ResponseWriter, r *http.Request) {
	clientID := ws.resolveClientID(r)
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	if ctx == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	activeChatID := ctx.getActiveChatID()
	cs := ctx.getChatSession(activeChatID)
	if cs == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	cs.mu.Lock()
	overrides := cs.ConfigOverrides
	if overrides == nil {
		overrides = make(map[string]interface{})
	}
	cs.mu.Unlock()
	writeJSON(w, http.StatusOK, overrides)
}

// handleGetProvenanceSettings returns per-key source information for the effective config.
// Response shape: { "sources": { "key": "global"|"project folder"|"session" } }
func (ws *ReactWebServer) handleGetProvenanceSettings(w http.ResponseWriter, r *http.Request) {
	// Load global config
	var globalCfg configuration.Config
	if configPath, err := configuration.GetConfigPath(); err == nil {
		if data, err := os.ReadFile(configPath); err == nil {
			_ = json.Unmarshal(data, &globalCfg)
		}
	}

	// Load project folder config
	var projectFolderCfg configuration.Config
	projectFolder := ws.getProjectFolderForRequest(r)
	if projectFolder != "" {
		if data, err := os.ReadFile(configuration.GetProjectFolderConfigPath(projectFolder)); err == nil {
			_ = json.Unmarshal(data, &projectFolderCfg)
		}
	}

	// Load session overrides
	clientID := ws.resolveClientID(r)
	var sessionOverrides map[string]interface{}
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		if cs := ctx.getChatSession(ctx.getActiveChatID()); cs != nil {
			cs.mu.Lock()
			if cs.ConfigOverrides != nil {
				sessionOverrides = make(map[string]interface{}, len(cs.ConfigOverrides))
				for k, v := range cs.ConfigOverrides {
					sessionOverrides[k] = v
				}
			}
			cs.mu.Unlock()
		}
	}
	ws.mutex.RUnlock()

	// Get effective merged config
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}
	effective := cm.GetConfig()

	// Build provenance map
	sources := make(map[string]string)

	// Session overrides take highest priority (both original and expanded keys)
	expandedSession := expandNestedKeys(sessionOverrides)
	for k := range expandedSession {
		sources[k] = "session"
	}

	// Compare project folder vs global (using expanded maps for nested keys).
	// Merge in defaults from a fresh config so that explicit project folder zero
	// values (e.g., reasoning_effort="") match the implicit global default
	// instead of being falsely attributed to "project folder".
	globalJSON, _ := json.Marshal(globalCfg)
	projectFolderJSON, _ := json.Marshal(projectFolderCfg)
	var globalMap, projectFolderMap map[string]interface{}
	_ = json.Unmarshal(globalJSON, &globalMap)
	_ = json.Unmarshal(projectFolderJSON, &projectFolderMap)

	defaultJSON, _ := json.Marshal(configuration.NewConfig())
	var defaultMap map[string]interface{}
	_ = json.Unmarshal(defaultJSON, &defaultMap)

	expandedGlobal := expandNestedKeys(globalMap)
	for k, v := range expandNestedKeys(defaultMap) {
		if _, ok := expandedGlobal[k]; !ok {
			expandedGlobal[k] = v
		}
	}
	expandedProjectFolder := expandNestedKeys(projectFolderMap)

	for k, wv := range expandedProjectFolder {
		if _, isSession := sources[k]; isSession {
			continue
		}
		wvBytes, _ := json.Marshal(wv)
		if gv, ok := expandedGlobal[k]; ok {
			gvBytes, _ := json.Marshal(gv)
			if string(wvBytes) != string(gvBytes) && string(wvBytes) != "null" {
				sources[k] = "workspace"
			}
		} else if wv != nil {
			sources[k] = "workspace"
		}
	}

	// Everything else is global (using expanded effective map)
	effectiveJSON, _ := json.Marshal(effective)
	var effectiveMap map[string]interface{}
	_ = json.Unmarshal(effectiveJSON, &effectiveMap)
	expandedEffective := expandNestedKeys(effectiveMap)
	for k := range expandedEffective {
		if _, exists := sources[k]; !exists {
			sources[k] = "global"
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sources": sources,
	})
}
