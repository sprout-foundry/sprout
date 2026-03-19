package webcontent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNopRenderer_ReturnsError verifies that the no-op renderer returns
// an error and does not panic. This tests the nopRenderer type directly
// (via the package-level nop variable) so it passes with and without
// the browser build tag.
func TestNopRenderer_ReturnsError(t *testing.T) {
	r := nop
	_, err := r.RenderPage(context.Background(), "https://example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
	r.Close() // should not panic
}

// TestNopRenderer_CloseIdempotent verifies that calling Close multiple
// times on the no-op renderer never panics.
func TestNopRenderer_CloseIdempotent(t *testing.T) {
	r := nop
	r.Close()
	r.Close()
	r.Close()
}

// TestNewBrowserRenderer_ReturnsRenderer verifies that NewBrowserRenderer
// returns a non-nil BrowserRenderer satisfying the interface.
func TestNewBrowserRenderer_ReturnsRenderer(t *testing.T) {
	r := NewBrowserRenderer()
	assert.NotNil(t, r)
	// Must satisfy the interface (compile-time check handled by browser.go,
	// but calling Close verifies it's usable).
	r.Close()
}
