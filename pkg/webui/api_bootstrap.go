//go:build !js

package webui

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
