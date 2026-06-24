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

	// SharedMode is true when the server shares the CLI's agent instance
	// (non-daemon interactive mode). The frontend uses this to hide
	// multi-chat UI and show "coupled with terminal" messaging.
	SharedMode bool `json:"sharedMode"`
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
	// appMode is always "local" from this binary. The launchd/systemd
	// "service mode" is still a self-hosted local install — the daemon
	// has a real workspace ($HOME) and the user reaches it via
	// localhost. Conflating it with "cloud" caused the frontend's
	// CloudAdapter to short-circuit /api/workspace with the synthetic
	// /home/user response (cloudEndpointRegistry/endpoints/synthetic.ts)
	// instead of calling the real local daemon. Managed cloud
	// deployments override via VITE_SPROUT_MODE at build time
	// (bootstrapAdapter.ts).
	appMode := "local"
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
		SharedMode:   ws.IsSharedMode(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}
