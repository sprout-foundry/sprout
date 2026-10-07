// Cloudflare adapter selection: pick the Pages or Workers adapter for a
// project from the deploy target its starter manifest declares.
//
// A Cloudflare deploy is one of two shapes — static output on Pages or
// server-side code on Workers — and the choice lives in the starter manifest's
// deploy_target field (absent means "pages"). This is a small, pure selector:
// it holds no I/O of its own, so the CLI and the deploy tools can share it and
// unit-test it without a network, an account, or a credential store.
package deploy

import (
	"fmt"
	"net/http"
)

// Deploy target ids accepted by CloudflareTargetFor.
const (
	// DeployTargetPages selects the Cloudflare Pages adapter for static build
	// output. It is also the default when no target is named.
	DeployTargetPages = "pages"
	// DeployTargetWorkers selects the Cloudflare Workers adapter for
	// server-side build output.
	DeployTargetWorkers = "workers"
)

// NormalizeDeployTarget returns the effective deploy target for a manifest
// value: an absent (empty) value means DeployTargetPages. Any other value is
// returned unchanged — including a whitespace-only or otherwise unrecognised
// one — so it is reported as an error rather than silently defaulted. This
// mirrors the manifest validator, which requires a present value to be exactly
// "pages" or "workers".
func NormalizeDeployTarget(deployTarget string) string {
	if deployTarget == "" {
		return DeployTargetPages
	}
	return deployTarget
}

// CloudflareTargetFor builds the Cloudflare adapter the deploy target selects:
// the Pages adapter for "pages" (or an empty value, the default) and the
// Workers adapter for "workers". Any other value — including a whitespace-only
// or otherwise unrecognised one — is an actionable error naming the accepted
// values, mirroring the manifest validator.
//
// It forwards the constructor seams unchanged (the API base URL and HTTP
// client, defaulted by the constructors), so tests point them at an httptest
// server exactly as they do for the constructors directly.
func CloudflareTargetFor(deployTarget string, cfg CloudflareConfig, cred Credential, baseURL string, client *http.Client) (DeployTarget, error) {
	switch NormalizeDeployTarget(deployTarget) {
	case DeployTargetPages:
		return NewCloudflarePagesTarget(cfg, cred, baseURL, client)
	case DeployTargetWorkers:
		return NewCloudflareWorkersTarget(cfg, cred, baseURL, client)
	default:
		return nil, fmt.Errorf("cloudflare: unknown deploy target %q (want %q or %q)",
			deployTarget, DeployTargetPages, DeployTargetWorkers)
	}
}
