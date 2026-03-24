package webui

import (
	"encoding/json"
	"net/http"
)

const maxHotkeysBodyBytes = 64 << 10 // 64 KiB

// handleAPIHotkeys dispatches GET and PUT /api/hotkeys
func (ws *ReactWebServer) handleAPIHotkeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPIHotkeysGet(w, r)
	case http.MethodPut:
		ws.handleAPIHotkeysPut(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPIHotkeysValidate handles POST /api/hotkeys/validate
func (ws *ReactWebServer) handleAPIHotkeysValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxHotkeysBodyBytes)

	var config HotkeyConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if err := ValidateHotkeyConfig(&config); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Return the validated config
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":   true,
		"config":  config,
	})
}

// handleAPIHotkeysGet returns the current hotkeys configuration
func (ws *ReactWebServer) handleAPIHotkeysGet(w http.ResponseWriter, r *http.Request) {
	config, err := LoadHotkeys()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to load hotkeys: "+err.Error())
		return
	}

	path, _ := GetHotkeysPath()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version": config.Version,
		"hotkeys": config.Hotkeys,
		"path":    path,
	})
}

// handleAPIHotkeysPut saves the hotkeys configuration
func (ws *ReactWebServer) handleAPIHotkeysPut(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxHotkeysBodyBytes)

	var config HotkeyConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	// Validate the configuration
	if err := ValidateHotkeyConfig(&config); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Save the configuration
	if err := SaveHotkeys(&config); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to save hotkeys: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"config":  config,
	})
}

// hotkeysPresetsRequest is the request body for POST /api/hotkeys/preset
type hotkeysPresetsRequest struct {
	Preset string `json:"preset"`
}

// handleAPIHotkeysPreset handles POST /api/hotkeys/preset.
// It generates a hotkey configuration from the named preset, validates it,
// saves it to disk, and returns the result.
func (ws *ReactWebServer) handleAPIHotkeysPreset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxHotkeysBodyBytes)

	var req hotkeysPresetsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.Preset == "" {
		writeJSONError(w, http.StatusBadRequest, "preset field is required")
		return
	}

	config := HotkeyPresetConfig(req.Preset)

	if err := ValidateHotkeyConfig(config); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Generated preset config failed validation: "+err.Error())
		return
	}

	if err := SaveHotkeys(config); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to save hotkeys: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"preset":  req.Preset,
		"config":  config,
	})
}
