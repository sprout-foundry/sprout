//go:build !js

package appcheck

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// TestWebcontentBrowserSharesOneRenderer pins the property that keeps repeated
// checks from starting a Chromium each: every browser the seam hands out drives
// the same process-lifetime renderer, so a check never owns a browser that
// nothing would close.
func TestWebcontentBrowserSharesOneRenderer(t *testing.T) {
	first, ok := NewWebcontentBrowser().(webcontentBrowser)
	if !ok {
		t.Fatal("NewWebcontentBrowser returned an unexpected type")
	}
	second, ok := NewWebcontentBrowser().(webcontentBrowser)
	if !ok {
		t.Fatal("NewWebcontentBrowser returned an unexpected type")
	}
	if first.renderer != second.renderer {
		t.Fatal("two browsers drove different renderers; a per-check renderer would leak a Chromium per run")
	}
	if first.renderer != webcontent.GetDirectBrowser() {
		t.Fatal("the browser did not drive pkg/webcontent's shared proxy-free renderer")
	}
}
