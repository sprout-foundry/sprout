//go:build !js

package verify

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// A dev server bound only to the IPv6 loopback (as Astro and Vite are on
// recent Node) must still be detected as up, and its route URLs must reach it.
func TestProbeDevPort_IPv6OnlyServerIsUp(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })

	port := l.Addr().(*net.TCPAddr).Port
	if !probeDevPort(port) {
		t.Fatalf("probeDevPort(%d) = false for a server listening on [::1] only", port)
	}
	resp, err := http.Get(routeURL(port, "/about/"))
	if err != nil {
		t.Fatalf("route URL did not reach the IPv6-only server: %v", err)
	}
	_ = resp.Body.Close()
}

// A server bound only to 127.0.0.1 keeps working.
func TestProbeDevPort_IPv4OnlyServerIsUp(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })

	if !probeDevPort(l.Addr().(*net.TCPAddr).Port) {
		t.Fatal("probeDevPort = false for a server listening on 127.0.0.1 only")
	}
}
