// Package agent provides error definitions for the agent package.
package agent

import "errors"

// Sentinel errors for the agent package.
// These are package-level error variables that can be used with errors.Is().

var (
	// errProviderStartupClosed is returned when provider startup is canceled by the user.
	errProviderStartupClosed = errors.New("provider startup canceled by user")
)
