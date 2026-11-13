// Package netproxy routes sprout's own outbound HTTP through a proxy set in
// SPROUT_HTTP_PROXY: its web tools (fetch, search, downloads) and the
// browser it launches. The setting is sprout's alone: it is not exported as
// HTTPS_PROXY, so the commands sprout runs (a project's tests, its package
// manager) see the environment they always had. Loopback addresses stay
// direct, so a local model server or a dev server on the machine is reached
// as before.
package netproxy

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Env is the variable that names the proxy, as a URL ("http://127.0.0.1:8082").
const Env = "SPROUT_HTTP_PROXY"

// URL is the configured proxy, or nil when none is set or it does not parse.
func URL() *url.URL {
	raw := strings.TrimSpace(os.Getenv(Env))
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}

// ProxyFunc is an http.Transport Proxy that sends requests through proxy,
// except to loopback hosts.
func ProxyFunc(proxy *url.URL) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if isLoopback(req.URL.Hostname()) {
			return nil, nil
		}
		return proxy, nil
	}
}

// Apply routes http.DefaultTransport through the configured proxy. It
// reports whether one was set.
func Apply() bool {
	proxy := URL()
	if proxy == nil {
		return false
	}
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.Proxy = ProxyFunc(proxy)
	}
	return true
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
