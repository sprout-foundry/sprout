//go:build js

package tools

// WASM stubs for SP-140 Phase 3 (see vision_delegate.go / url_sniff.go).
// The WASM shell degrades to inline-only vision: delegation requires a
// remote vision client created through the provider factory, which the
// browser build does not wire up.

import (
	"context"
	"errors"
)

var errDelegationUnavailable = errors.New("image delegation is not available in WASM mode")

// DelegatedImage is a WASM stub type — real definition in vision_delegate.go.
type DelegatedImage struct {
	Path        string
	Description string
	Provider    string
	Model       string
}

// DelegateImageDescriptions — WASM stub. Delegation is unavailable in the
// browser build; callers fall back to the analyze-tool prompt.
func DelegateImageDescriptions(_ context.Context, _ interface{}, _ string) (DelegatedImage, error) {
	return DelegatedImage{}, errDelegationUnavailable
}

// SniffURLContent — WASM stub. Content sniffing is unavailable in the
// browser build; unknown-content-type URLs take the text path.
func SniffURLContent(_ context.Context, _ string) (ResponseKind, string, bool) {
	return ResponseKindUnknown, "", false
}
