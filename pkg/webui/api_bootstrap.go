//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
)

// RuntimeConfig provides runtime configuration for the web UI.
// Served via GET /api/bootstrap (unauthenticated) so the frontend
// can configure itself without hardcoded values.
type RuntimeConfig struct {
	// APIBaseURL is the base URL for API requests (e.g., "http://localhost:56000").
	APIBaseURL string `json:"apiBaseURL"`

	// WSURL is the WebSocket URL for real-time updates.
	WSURL string `json:"wsURL"`

	// AuthMode controls authentication: "none" (local), "bearer" (cloud/token).
	AuthMode string `json:"authMode"`

	// AppMode is the application mode: "local" (desktop/self-hosted), "cloud" (managed).
	AppMode string `json:"appMode"`

	// BuildVersion is the version string embedded at build time.
	BuildVersion string `json:"buildVersion"`
}

func (ws *ReactWebServer) handleAPIBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authMode := "none"
	if ws.authToken != "" {
		authMode = "bearer"
	}
	appMode := "local"
	if ws.serviceMode {
		appMode = "cloud"
	}
	scheme := "http"
	wsScheme := "ws"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
		wsScheme = "wss"
	}
	host := r.Host
	config := RuntimeConfig{
		APIBaseURL:   scheme + "://" + host,
		WSURL:        wsScheme + "://" + host + "/ws",
		AuthMode:     authMode,
		AppMode:      appMode,
		BuildVersion: "dev",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}
