//go:build !js

package appcheck

import (
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// hostedApp is the running instance of the build output the browser is
// pointed at: either wrangler dev serving dist/worker.js with the hosted
// configuration, or the built-in static server for a build without a worker.
type hostedApp struct {
	// BaseURL is the origin the browser visits ("http://127.0.0.1:<port>").
	BaseURL string
	// HasServer reports whether the worker is being served.
	HasServer bool
	// log collects wrangler's output (empty for the static server).
	log *logSink
}

// Log returns the server output collected so far.
func (a *hostedApp) Log() string {
	if a == nil || a.log == nil {
		return ""
	}
	return a.log.String()
}

// serveOptions configures one serve.
type serveOptions struct {
	// Root is the project root.
	Root string
	// Dist is the build output directory.
	Dist string
	// Wrangler is the wrangler binary to run, or "" for none.
	Wrangler string
}

// readToml returns the project's wrangler.toml, or "" when it has none.
func readToml(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "wrangler.toml")) //nolint:gosec // G304: the project's own config
	if err != nil {
		return ""
	}
	return string(data)
}

// mustRead reads a small file the caller has already established exists.
func mustRead(path string) []byte {
	data, _ := os.ReadFile(path) //nolint:gosec // G304: the project's own build output
	return data
}

// tail returns the last n bytes of s, so a failure report keeps the end of a
// server log where the error is.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// serverStartTimeout bounds how long the check waits for the server to come
// up: a cold wrangler start (bundling, migrations) is slow but bounded.
const serverStartTimeout = 60 * time.Second

// serverProbeInterval is the pause between readiness probes.
const serverProbeInterval = 500 * time.Millisecond

// waitFor polls url until it answers (any HTTP status counts as up) or the
// timeout elapses.
func waitFor(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		resp, err := client.Get(url) //nolint:gosec // G107: the loopback URL this check just started
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(serverProbeInterval)
	}
}
