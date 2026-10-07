package preview

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// devURL is the embed URL the preview pane gets for a dev server on port:
// a localhost origin, so the preview pane can embed localhost.
func devURL(port int) string {
	return "http://localhost:" + strconv.Itoa(port)
}

// probeURL is the loopback URL the readiness and liveness probes hit
// (127.0.0.1, the address dev servers bind; verify's probe convention).
func probeURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", port)
}

// probeClientTimeout bounds a single dev-port probe so a stuck socket
// never blocks the readiness or liveness loops.
const probeClientTimeout = 500 * time.Millisecond

// probeHTTP reports whether anything on port answers a plain HTTP GET
// (verify's dev-port probe): any status code — including 404 — means the
// server is up.
func probeHTTP(port int) bool {
	client := &http.Client{Timeout: probeClientTimeout}
	resp, err := client.Get(probeURL(port))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// PortServing reports whether anything answers a plain HTTP GET on port
// (any status counts — the dev-port probe). Callers outside the package
// use it for the detection fast path (the webui start handler) so a dev
// server already up on the manifest's port is reported synchronously
// instead of after a start that would find it mid-flight.
func PortServing(port int) bool {
	return probeHTTP(port)
}

// excerptMaxBytes bounds the dev output tail carried in a failure reason.
const excerptMaxBytes = 200

// outputExcerpt renders the tail of the dev server's captured output (""
// when nothing was captured) so a failure reason says why, not just that.
// It is only safe to read after the process has been reaped.
func outputExcerpt(out *bytes.Buffer) string {
	if out == nil {
		return ""
	}
	b := out.Bytes()
	if len(b) == 0 {
		return ""
	}
	start := len(b) - excerptMaxBytes
	if start < 0 {
		start = 0
	}
	return " (last output: " + string(b[start:]) + ")"
}
