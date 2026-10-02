//go:build !js

package webui

// settings_api_put_configfile.go — the config-file write layer behind the
// settings PUT API: putConfigToFile + the partial-apply helpers
// (applyPartialSettings, expandDottedKeys, configAsMap, cloneStringMap,
// setNestedKey). Split out of settings_api_put.go.
import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// putConfigToFile is a helper that merges incoming settings into an existing
// config file and writes the result back.
func (ws *ReactWebServer) putConfigToFile(w http.ResponseWriter, r *http.Request, configPath string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)
	var incoming map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Load existing config or create default
	var cfg configuration.Config
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &cfg)
	} else {
		cfg = *configuration.NewConfig()
	}

	// Map session-style provider/model shortcuts to persisted config fields.
	// The frontend sends "provider" and "model" regardless of layer, but
	// applyPartialSettings expects "last_used_provider" and "provider_models".
	// Always delete these keys from incoming so they're never reported as unknown.
	if p, ok := incoming["provider"]; ok {
		if ps, ok := p.(string); ok {
			if ps != "" {
				incoming["last_used_provider"] = ps
			} else {
				// Empty string means clear the provider
				incoming["last_used_provider"] = ""
			}
		}
		delete(incoming, "provider")
	}
	if m, ok := incoming["model"]; ok {
		if ms, ok := m.(string); ok && ms != "" {
			// Determine which provider this model belongs to.
			provider := ""
			if p, ok := incoming["last_used_provider"].(string); ok && p != "" {
				provider = p
			} else if cfg.LastUsedProvider != "" {
				provider = cfg.LastUsedProvider
			}
			if provider != "" {
				if cfg.ProviderModels == nil {
					cfg.ProviderModels = make(map[string]string)
				}
				pm := make(map[string]interface{}, len(cfg.ProviderModels))
				for k, v := range cfg.ProviderModels {
					pm[k] = v
				}
				pm[provider] = ms
				incoming["provider_models"] = pm
			}
		}
		delete(incoming, "model")
	}

	// Apply patch and collect unknown keys
	unknownKeys, err := applyPartialSettings(&cfg, incoming)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Cannot create config directory: %v", err))
		return
	}

	// Write
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to marshal config: %v", err))
		return
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to write config: %v", err))
		return
	}

	// Fold the layer write back into the live config manager. The file write
	// above bypasses the manager, so without this the merged in-memory config
	// (what every agent reads via GetConfig) keeps the previous value until
	// the next restart — the save "worked" but nothing running saw it.
	if cm := ws.resolveConfigManagerQuietly(r); cm != nil {
		if err := cm.Reload(); err != nil {
			ws.log().Warn("layered settings write: manager reload failed", slog.Any("err", err))
		}
	}

	// If the patch contained a primary provider/model change, also apply
	// it to the live agent and republish provider state. Without this,
	// the on-disk config is correct but the active session keeps running
	// against the old model — the dropdown "looks broken" because the
	// status bar doesn't move until the user reloads.
	newProvider, _ := incoming["last_used_provider"].(string)
	var newModel string
	if pm, ok := incoming["provider_models"].(map[string]interface{}); ok && newProvider != "" {
		if m, ok := pm[newProvider].(string); ok {
			newModel = m
		}
	}
	if newProvider != "" || newModel != "" {
		clientID := ws.resolveClientID(r)
		// Resolve the active chat ID so we apply to the correct per-chat agent.
		activeChatID := ""
		ws.mutex.RLock()
		if ctx := ws.clientContexts[clientID]; ctx != nil {
			activeChatID = ctx.getActiveChatID()
		}
		ws.mutex.RUnlock()

		if activeChatID != "" {
			if agentInst, err := ws.getChatAgent(clientID, activeChatID); err == nil && agentInst != nil {
				if newProvider != "" {
					if cm := ws.getConfigManager(r, w); cm != nil {
						if pt, err := cm.MapStringToClientType(newProvider); err == nil {
							if err := agentInst.SetProvider(pt); err != nil {
								ws.log().Warn("failed to set provider on live agent after persisted settings update", slog.Any("err", err))
							}
						}
					}
				}
				if newModel != "" {
					if err := agentInst.SetModel(newModel); err != nil {
						ws.log().Warn("failed to set model on live agent after persisted settings update", slog.Any("err", err))
					}
				}
			}
		}
		ws.publishProviderState(clientID)
		// Warn the user if the persisted provider needs a credential it
		// doesn't have — same warning path as the websocket
		// provider_change handler so the UX is consistent across all the
		// surfaces that can swap the active provider.
		if newProvider != "" {
			activeChatID := ""
			ws.mutex.RLock()
			if ctx := ws.clientContexts[clientID]; ctx != nil {
				activeChatID = ctx.getActiveChatID()
			}
			ws.mutex.RUnlock()
			ws.notifyMissingCredentialIfNeeded(clientID, activeChatID, newProvider)
		}
	}

	resp := map[string]interface{}{
		"success": true,
		"config":  sanitizedConfig(&cfg),
	}
	if len(unknownKeys) > 0 {
		resp["warnings"] = []string{fmt.Sprintf("Unknown fields ignored: %v", unknownKeys)}
	}
	writeJSON(w, http.StatusOK, resp)
}

// applyPartialSettings applies a partial JSON patch to the config struct.
// Only whitelisted top-level keys are accepted to prevent accidental
// overwrite of internal bookkeeping fields.
// Unknown keys are collected and returned so callers can warn the user.
// applyPartialSettings applies a partial JSON patch to the config struct.
// Only whitelisted top-level keys are accepted to prevent accidental
// overwrite of internal bookkeeping fields.
// Unknown keys are collected and returned so callers can warn the user.
//
// The actual per-domain work lives in settings_api_partial_settings.go so
// each domain can be reasoned about in isolation. This orchestrator just
// walks the appliers in order and collects any patch keys that none of
// them recognized.
func applyPartialSettings(cfg *configuration.Config, patch map[string]interface{}) ([]string, error) {
	patch = expandDottedKeys(cfg, patch)

	knownKeys := make(map[string]bool, len(patch))
	for _, apply := range partialSettingsAppliers {
		if err := apply(cfg, patch, knownKeys); err != nil {
			return nil, err
		}
	}
	var unknown []string
	for k := range patch {
		if !knownKeys[k] {
			unknown = append(unknown, k)
		}
	}
	return unknown, nil
}

// expandDottedKeys rewrites "section.field" patch keys into a whole "section"
// object seeded from the config's current values.
//
// Two constraints meet here. The webui saves one field at a time
// (updateSetting('computer_use.enabled', v) puts {"computer_use.enabled":true}
// on the wire), but every section applier rebuilds its struct wholesale from
// patch["computer_use"]. So a dotted key matched no applier and the write was
// dropped — surfaced only in a "warnings" field the client never reads, behind a
// 200 and a green "Saved" toast. Seeding from the current values is what keeps
// the expansion from trading that silent no-op for a silent wipe of the field's
// siblings.
func expandDottedKeys(cfg *configuration.Config, patch map[string]interface{}) map[string]interface{} {
	sections := map[string]map[string]interface{}{}
	var current map[string]interface{}

	for key, value := range patch {
		section, path, dotted := strings.Cut(key, ".")
		if !dotted {
			continue
		}
		if _, seeded := sections[section]; !seeded {
			if current == nil {
				current = configAsMap(cfg)
			}
			seed, _ := current[section].(map[string]interface{})
			sections[section] = cloneStringMap(seed)
		}
		setNestedKey(sections[section], path, value)
	}

	if len(sections) == 0 {
		return patch
	}

	expanded := make(map[string]interface{}, len(patch))
	for key, value := range patch {
		if !strings.Contains(key, ".") {
			expanded[key] = value
		}
	}
	for section, fields := range sections {
		expanded[section] = fields
	}
	return expanded
}

func configAsMap(cfg *configuration.Config) map[string]interface{} {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func cloneStringMap(src map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(src)+1)
	for k, v := range src {
		out[k] = v
	}
	return out
}

// setNestedKey assigns value at a dotted path, creating intermediate maps and
// replacing any non-map value blocking the way.
func setNestedKey(target map[string]interface{}, path string, value interface{}) {
	field, rest, nested := strings.Cut(path, ".")
	if !nested {
		target[field] = value
		return
	}
	child, ok := target[field].(map[string]interface{})
	if !ok {
		child = map[string]interface{}{}
	} else {
		child = cloneStringMap(child)
	}
	setNestedKey(child, rest, value)
	target[field] = child
}
