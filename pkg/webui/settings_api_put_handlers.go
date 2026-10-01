//go:build !js

package webui

// settings_api_put_handlers.go — the per-scope PUT handlers for the
// settings API: handlePutSessionSettings, handlePutWorkspaceSettings,
// handlePutGlobalSettings (request decoding + validation, delegating the
// config-file write to the settings_api_put_configfile.go layer). Split
// out of settings_api_put.go.
import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// handlePutSessionSettings writes settings to the current session's ConfigOverrides.
func (ws *ReactWebServer) handlePutSessionSettings(w http.ResponseWriter, r *http.Request) {
	clientID := ws.resolveClientID(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)
	var incoming map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate provider if included
	if p, ok := incoming["provider"].(string); ok && p != "" {
		cm := ws.getConfigManager(r, w)
		if cm == nil {
			return
		}
		if _, err := cm.MapStringToClientType(p); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid provider: %v", err))
			return
		}
	}

	// Check for unknown keys and collect warnings
	knownSessionKeys := map[string]bool{
		"provider":           true,
		"model":              true,
		"temperature":        true,
		"max_tokens":         true,
		"reasoning_effort":   true,
		"system_prompt_text": true,
		"skip_prompt":        true,
		"web_search_enabled": true,
		"subagent_provider":  true,
		"subagent_model":     true,
		"disable_thinking":   true,
		"top_p":              true,
		"frequency_penalty":  true,
		"presence_penalty":   true,
		"stop_sequences":     true,
		"tool_choice":        true,
		"response_format":    true,
		"stream":             true,
	}

	var unknownKeys []string
	for k := range incoming {
		if !knownSessionKeys[k] {
			unknownKeys = append(unknownKeys, k)
		}
	}

	// Merge into session ConfigOverrides
	ws.mutex.Lock()
	ctx := ws.clientContexts[clientID]
	var cs *chatSession
	if ctx != nil {
		cs = ctx.getChatSession(ctx.getActiveChatID())
	}
	if ctx == nil || cs == nil {
		ws.mutex.Unlock()
		writeJSONError(w, http.StatusBadRequest, "No active session")
		return
	}
	cs.mu.Lock()
	if cs.ConfigOverrides == nil {
		cs.ConfigOverrides = make(map[string]interface{})
	}
	for k, v := range incoming {
		// Auto-truncate string values before storing
		if s, ok := v.(string); ok {
			switch k {
			case "provider", "model", "subagent_provider", "subagent_model":
				v = truncateString(s, maxSettingNameLength)
			case "reasoning_effort":
				v = truncateString(s, maxSettingEnumLength)
			case "system_prompt_text":
				v = truncateString(s, maxSettingPromptLength)
			default:
				v = truncateString(s, maxSettingGenericLength)
			}
		}
		if v == nil || v == "" || v == 0 || v == false {
			delete(cs.ConfigOverrides, k)
		} else {
			cs.ConfigOverrides[k] = v
		}
	}
	// Sync Provider/Model shortcuts
	if p, ok := cs.ConfigOverrides["provider"].(string); ok {
		cs.Provider = p
	}
	if m, ok := cs.ConfigOverrides["model"].(string); ok {
		cs.Model = m
	}
	savedOverrides := make(map[string]interface{}, len(cs.ConfigOverrides))
	for k, v := range cs.ConfigOverrides {
		savedOverrides[k] = v
	}
	cs.mu.Unlock()
	ws.mutex.Unlock()

	// Apply to live chat agent (not client-level agent) so the override
	// reaches the correct per-chat instance. Skip if the chat has an
	// active query — SetProvider swaps a.client without synchronization.
	providerOrModelChanged := false
	activeChatIDForAgent := ""
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		activeChatIDForAgent = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	canChangeAgent := activeChatIDForAgent != ""
	if canChangeAgent {
		ws.mutex.RLock()
		ctx := ws.clientContexts[clientID]
		if ctx != nil && ctx.hasActiveQueryForChat(activeChatIDForAgent) {
			canChangeAgent = false
		}
		ws.mutex.RUnlock()
	}

	if canChangeAgent {
		if agentInst, err := ws.getChatAgent(clientID, activeChatIDForAgent); err == nil && agentInst != nil {
			if p, ok := savedOverrides["provider"].(string); ok && p != "" {
				cm := ws.getConfigManager(r, w)
				if cm != nil {
					if pt, err := cm.MapStringToClientType(p); err == nil {
						agentInst.SetProvider(pt)
						providerOrModelChanged = true
					}
				}
			}
			if m, ok := savedOverrides["model"].(string); ok && m != "" {
				agentInst.SetModel(m)
				providerOrModelChanged = true
			}
			agentInst.SetConfigOverrides(savedOverrides)
		}
	}

	// Refresh the status bar for any provider/model change in session
	// scope, matching the default handler's behavior so the WebUI never
	// looks "stuck" on a stale model after the user picks a new one.
	if providerOrModelChanged {
		ws.publishProviderState(clientID)
		if p, ok := savedOverrides["provider"].(string); ok && p != "" {
			activeChatID := ""
			ws.mutex.RLock()
			if ctx := ws.clientContexts[clientID]; ctx != nil {
				activeChatID = ctx.getActiveChatID()
			}
			ws.mutex.RUnlock()
			ws.notifyMissingCredentialIfNeeded(clientID, activeChatID, p)
		}
	}

	resp := map[string]interface{}{
		"success": true,
		"config":  savedOverrides,
	}
	if len(unknownKeys) > 0 {
		resp["warnings"] = []string{fmt.Sprintf("Unknown fields ignored: %v", unknownKeys)}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePutWorkspaceSettings writes settings to the workspace config file.
func (ws *ReactWebServer) handlePutWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		writeJSONError(w, http.StatusBadRequest, "No workspace configured")
		return
	}
	// Write path, not the read path: a legacy config.json is read but never
	// written back to.
	//
	// Empty means this workspace has no config layer — currently only $HOME,
	// which deliberately has none (see configuration.WorkspaceConfigDir).
	// Writing there would create ~/.sprout/workspace.json, which the next
	// startup reads back as a per-workspace opt-in. Direct the caller at the
	// global scope instead of silently writing nothing.
	writePath := configuration.WorkspaceConfigWritePath(workspaceRoot)
	if writePath == "" {
		writeJSONError(w, http.StatusBadRequest,
			"This workspace has no per-workspace settings layer (the home directory uses global settings). Save to global settings instead.")
		return
	}
	ws.putConfigToFile(w, r, writePath)
}

// handlePutGlobalSettings writes settings to the global config file.
func (ws *ReactWebServer) handlePutGlobalSettings(w http.ResponseWriter, r *http.Request) {
	configPath, err := configuration.GetConfigPath()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Cannot determine global config path")
		return
	}
	ws.putConfigToFile(w, r, configPath)
}
