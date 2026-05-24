package llmproxy

import (
	"fmt"
	"strings"
)

// IsCORSError returns true if the error is likely a CORS-related failure.
// This is heuristic-based since browsers do not provide specific CORS error info.
func IsCORSError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	// Direct CORS mentions
	if strings.Contains(msg, " cors ") {
		return true
	}
	if strings.Contains(msg, "cross-origin") {
		return true
	}
	if strings.Contains(msg, "blocked by cors policy") {
		return true
	}

	// Generic browser fetch errors that often indicate CORS
	if strings.Contains(msg, "failed to fetch") {
		return true
	}
	if strings.Contains(msg, "networkerror") {
		return true
	}
	if strings.Contains(msg, "load failed") {
		return true
	}

	// TypeError combined with fetch (common CORS indicator in browsers)
	if strings.Contains(msg, "typeerror") && strings.Contains(msg, "fetch") {
		return true
	}

	return false
}

// CORSErrorMessage returns a user-friendly explanation of a CORS error,
// including guidance on how to resolve it.
func CORSErrorMessage(err error, provider string) string {
	base := err.Error()
	return fmt.Sprintf(
		"%s\n\n"+
			"This error may be caused by CORS (Cross-Origin Resource Sharing) restrictions. "+
			"The browser blocks direct requests to %s from the current page.\n\n"+
			"To resolve this, use SproutWasm.setPlatformEndpoint(url) to route through the Sprout platform, "+
			"or SproutWasm.setCorsProxy(proxyUrl) to use a custom CORS proxy.\n\n"+
			"Note: CORS errors in browsers are indistinguishable from genuine network errors. "+
			"If the provider is reachable from other contexts, CORS is the likely cause.",
		base,
		provider,
	)
}
