//go:build !js

package webui

import (
	"errors"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ErrNoProviderConfigured is returned by getClientAgent and getChatAgent
// when no AI provider is configured (e.g., the user skipped onboarding and
// set LastUsedProvider to "editor").
var ErrNoProviderConfigured = errors.New("no AI provider configured")

// isProviderAvailable checks whether a real AI provider is configured in the
// user's settings. Returns false only for "editor" mode (explicitly set).
// For empty provider, returns true to allow agent auto-selection during
// onboarding and tests. This is a lightweight config-only check — it does
// NOT attempt to create an agent or validate connection.
//
// The check must mirror the layering agent creation actually uses
// (global + workspace): when the workspace layer sets a real provider, the
// chat paths stay available even if the global layer says "editor" (e.g. the
// user pressed "use as editor" in another workspace). Reading global-only
// here gated chats in workspaces whose own config fully configures a provider.
func isProviderAvailable() bool {
	return isProviderAvailableInWorkspace("")
}

// isProviderAvailableInWorkspace is the workspace-aware variant. Pass an empty
// workspaceRoot for the global-only check (legacy call sites).
func isProviderAvailableInWorkspace(workspaceRoot string) bool {
	provider, ok := configuredProviderForWorkspace(workspaceRoot)
	if !ok {
		// Config load failed — let the full agent creation path surface
		// the actual error rather than masking it as "no provider".
		return true
	}
	// Only return false for explicitly set "editor" mode.
	// For empty provider, return true to allow agent auto-selection.
	return provider != "editor"
}

// configuredProviderForWorkspace resolves the effective provider the layered
// config (global + workspace) would select, without creating an agent.
// Returns ok=false when the layered config could not be loaded.
func configuredProviderForWorkspace(workspaceRoot string) (provider string, ok bool) {
	globalDir, err := configuration.GetConfigDir()
	if err != nil {
		return "", false
	}
	var workspaceDir string
	if workspaceRoot != "" {
		workspaceDir = configuration.WorkspaceConfigDir(workspaceRoot)
	}
	cm, err := configuration.NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		return "", false
	}
	cfg := cm.GetConfig()
	if cfg == nil {
		return "", false
	}
	return strings.TrimSpace(cfg.LastUsedProvider), true
}
