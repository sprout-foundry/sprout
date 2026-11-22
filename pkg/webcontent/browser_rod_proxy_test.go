//go:build !js

package webcontent

import (
	"testing"

	"github.com/go-rod/rod/lib/launcher"
)

// TestApplyProxyLaunchFlags pins which launcher branch the browser takes.
// Direct mode exists for callers that drive a server on this machine: a proxy
// configured for outbound traffic must never answer for their loopback
// requests, or every page comes back a proxy error. What the flags do to a
// real browser is pinned by TestDirectBrowserReachesLoopbackThroughAConfiguredProxy,
// which drives a page.
func TestApplyProxyLaunchFlags(t *testing.T) {
	cases := []struct {
		name     string
		direct   bool
		proxyURL string
		want     []string
	}{
		{name: "direct mode ignores the proxy", direct: true, proxyURL: "http://127.0.0.1:8082", want: []string{"no-proxy-server"}},
		{name: "direct mode with no proxy", direct: true, want: []string{"no-proxy-server"}},
		{name: "proxy shared with loopback direct", proxyURL: "http://127.0.0.1:8082", want: []string{"proxy-server", "proxy-bypass-list"}},
		{name: "no proxy configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyProxyLaunchFlags(launcher.New(), tc.direct, tc.proxyURL)
			if len(got) != len(tc.want) {
				t.Fatalf("flags %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("flags %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestDirectBrowserRendererIsIndependent guards the app check's browser: it
// renders through a renderer of its own, so one that cannot use a proxy is
// what the check asks for and a shared proxied renderer never serves it.
func TestDirectBrowserRendererIsIndependent(t *testing.T) {
	direct, ok := NewDirectBrowserRenderer().(*rodRenderer)
	if !ok {
		t.Fatal("NewDirectBrowserRenderer returned an unexpected type")
	}
	if !direct.directMode {
		t.Fatal("NewDirectBrowserRenderer did not enable direct mode")
	}
	shared, ok := NewBrowserRenderer().(*rodRenderer)
	if !ok {
		t.Fatal("NewBrowserRenderer returned an unexpected type")
	}
	if shared.directMode {
		t.Fatal("NewBrowserRenderer enabled direct mode")
	}
	if direct == shared {
		t.Fatal("NewDirectBrowserRenderer shared the default renderer")
	}
}

// TestCloseDirectBrowserRearms pins that closing the proxy-free renderer does
// not poison it: a caller that closes early gets a working renderer again
// rather than one that refuses every session (a closed renderer stays closed).
func TestCloseDirectBrowserRearms(t *testing.T) {
	first := GetDirectBrowser()
	if first == nil {
		t.Fatal("GetDirectBrowser returned nil")
	}
	CloseDirectBrowser()
	if GetDirectBrowser() == first {
		t.Fatal("GetDirectBrowser returned the closed renderer after CloseDirectBrowser")
	}
	CloseDirectBrowser()
	CloseDirectBrowser() // a second close must not panic or wedge the renderer
}
