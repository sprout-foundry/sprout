//go:build !browser

package webcontent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBrowseURL_TextAction_NopRenderer(t *testing.T) {
	t.Skip("native build has browser available; nop test not applicable")
}

func TestBrowseURL_DOMAction_NopRenderer(t *testing.T) {
	t.Skip("native build has browser available; nop test not applicable")
}

func TestBrowseURL_ScreenshotAction_NopRenderer(t *testing.T) {
	t.Skip("native build has browser available; nop test not applicable")
}

func TestBrowseURL_InspectAction_NopRenderer(t *testing.T) {
	t.Skip("native build has browser available; nop test not applicable")
}

func TestNopRenderer_Screenshot(t *testing.T) {
	r := nop
	err := r.Screenshot(nil, "http://example.com", "/tmp/test.png", 1280, 720, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestNopRenderer_CaptureDOM(t *testing.T) {
	r := nop
	_, err := r.CaptureDOM(nil, "http://example.com", 1280, 720, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestNopRenderer_Run(t *testing.T) {
	r := nop
	_, err := r.Run(nil, "http://example.com", BrowseOptions{Action: "inspect"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}
