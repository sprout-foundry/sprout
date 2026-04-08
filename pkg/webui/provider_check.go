package webui

import (
	"errors"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
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
func isProviderAvailable() bool {
	cfg, err := configuration.Load()
	if err != nil {
		// Config load failed — let the full agent creation path surface
		// the actual error rather than masking it as "no provider".
		return true
	}
	provider := strings.TrimSpace(cfg.LastUsedProvider)
	// Only return false for explicitly set "editor" mode.
	// For empty provider, return true to allow agent auto-selection.
	return provider != "editor"
}
