//go:build !js

package webui

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	gitops "github.com/sprout-foundry/sprout/pkg/git"
	"github.com/sprout-foundry/sprout/pkg/updatecheck"
)

// bootstrapSyncBudget bounds the total time spent computing the boot-time git
// snapshot. Each git call is already bounded by git.SyncGitTimeout; this
// caps the worst-case sum so a pathological repo can never stall
// /api/bootstrap.
const bootstrapSyncBudget = 10 * time.Second

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

	// Update is non-nil when the cached release check (shared with the
	// CLI, at most one GitHub lookup per day) has a newer stable version
	// than the running binary. The frontend renders a banner from it; nil
	// means "nothing to show" and is omitted in JSON.
	Update *UpdateInfo `json:"update,omitempty"`

	// SharedMode is true when the server shares the CLI's agent instance
	// (non-daemon interactive mode). The frontend uses this to hide
	// multi-chat UI and show "coupled with terminal" messaging.
	SharedMode bool `json:"sharedMode"`

	// Sync is the ETH-1 sync-on-resume git snapshot for the workspace:
	// branch, dirty files, ahead/behind and last commit at boot. It is
	// computed with the pull DISABLED — bootstrap must never mutate the
	// repo — so pull.result is always "not_attempted" here. nil (rendered
	// as "sync": null) when git state could not be determined; it never
	// fails the bootstrap response.
	Sync *gitops.SyncReport `json:"sync"`

	// PlatformURL (SP-016 P0.3) is the absolute base URL of the platform
	// web UI, so the editor's account-surface exits (the "← Dashboard"
	// back-link, escalation "View task on platform" links, avatar-menu
	// exits) can build absolute URLs instead of self-looping into this
	// daemon's own SPA on a Fly workspace (Mode B). Sourced from the
	// SPROUT_PLATFORM_URL env var (injected by the operator or the
	// workspace launch config); empty string means the field is omitted
	// (same pattern as other optional fields), and the editor keeps its
	// current relative-exit behavior.
	PlatformURL string `json:"platformURL,omitempty"`

	// EgressProxy is the URL of the platform's egress/CORS proxy that the
	// in-browser agent routes non-platform HTTP (GitHub git + REST) through.
	// Sourced from the SPROUT_EGRESS_PROXY env var; empty string omits the
	// field, and the agent falls back to the two-origin network restriction
	// (GitHub unreachable in-browser). See webui/src/types/runtimeConfig.ts.
	EgressProxy string `json:"egressProxy,omitempty"`

	// RepoURL is the repository the workspace should open, when the host
	// names one (a host's project repoUrl). Sourced from the SPROUT_REPO_URL
	// env var; empty string omits the field, and the editor falls back to the
	// ?repo= query parameter. A host that mounts a workspace supplies the repo
	// through its own transport, so this is the daemon-side equivalent for a
	// host that drives the bootstrap instead.
	RepoURL string `json:"repoUrl,omitempty"`

	// ContractVersion is the API contract version (the OpenAPI info.version
	// in docs/api) this daemon build serves. The Web UI compares its major
	// version to the value it was built against and refuses to start when
	// they are incompatible; a newer minor version is a warning only.
	ContractVersion string `json:"contractVersion"`
}

// UpdateInfo tells the frontend a newer release is available. It is a
// wire contract — field names are pinned by webui/src/types/runtimeConfig.ts.
type UpdateInfo struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
}

func (ws *ReactWebServer) handleAPIBootstrap(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
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
		BuildVersion: buildinfo.Version,
		Update:       updatePayload(),
		SharedMode:   ws.IsSharedMode(),
		Sync:         computeBootstrapSync(r.Context(), ws.getWorkspaceRootForRequest(r)),
		// The contract version this build serves (the source of truth in
		// contract.go); the frontend negotiates against its own pin.
		ContractVersion: ContractVersion,
		// SP-016 P0.3: absolute platform base for the editor's account-surface
		// exits. Empty (env unset) → the omitempty tag drops the field and
		// the editor keeps its current relative-exit behavior.
		PlatformURL: os.Getenv("SPROUT_PLATFORM_URL"),
		// Egress proxy the platform advertises for the in-browser agent's
		// GitHub traffic. Empty (env unset) → the omitempty tag drops it and
		// the agent keeps the two-origin restriction.
		EgressProxy: os.Getenv("SPROUT_EGRESS_PROXY"),
		// Repository the workspace should open, when the host names one.
		// Empty (env unset) → the omitempty tag drops it and the editor falls
		// back to the ?repo= query parameter.
		RepoURL: os.Getenv("SPROUT_REPO_URL"),
	}
	writeJSON(w, http.StatusOK, config)
}

// updatePayload reports the cached newer release, if any. The lookup has
// no side effects and never triggers a network call — the fetch only
// happens via the background refresh hooks.
func updatePayload() *UpdateInfo {
	latest, ok := updatecheck.CachedNewer(buildinfo.Version, time.Now())
	if !ok {
		return nil
	}
	return &UpdateInfo{Current: buildinfo.Version, Latest: latest}
}

// computeBootstrapSync builds the boot-time git snapshot via the same
// git.RunSync used by `sprout sync` and GET /api/sync, with the pull
// disabled (bootstrap never mutates the repo). Any failure — including the
// budget expiring — returns nil, which renders as "sync": null; bootstrap
// itself must never fail because of it.
func computeBootstrapSync(ctx context.Context, workspaceRoot string) *gitops.SyncReport {
	budgetCtx, cancel := context.WithTimeout(ctx, bootstrapSyncBudget)
	defer cancel()

	report, err := gitops.RunSync(budgetCtx, workspaceRoot, false)
	if err != nil {
		return nil
	}
	return &report
}
