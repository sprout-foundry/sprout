package verify

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

// DefaultDevServerTimeout bounds how long a page check waits for the
// manifest's dev server to come up on its port: thirty seconds covers a cold
// dev-server start without stalling a turn.
const DefaultDevServerTimeout = 30 * time.Second

// devServerPollInterval is the interval between dev-port readiness probes.
const devServerPollInterval = 250 * time.Millisecond

// devProbeClientTimeout bounds a single dev-port probe so a stuck socket
// never blocks the readiness loop.
const devProbeClientTimeout = 500 * time.Millisecond

// devServer is the lifecycle of one manifest dev command for a page check:
// start it, wait for readiness on its port, capture its combined output, and
// always stop it (even on failure or cancellation). It is the common
// orchestrator; the process-spawning and -stopping halves are the
// platform-specific DevProcess (devserver_process_*.go).
type devServer struct {
	proc *DevProcess
	out  *bytes.Buffer
	// done is closed once the process has been reaped (Wait returned),
	// which also guarantees the captured output is complete.
	done chan struct{}
}

// startDevServer starts devCommand in root, then polls the dev port every
// devServerPollInterval until it answers a request (any HTTP status means the
// server is up) or timeout elapses. It returns the server handle (the caller
// defers stopDevServer on it) and a failure reason: "" when the server is
// ready (still running), or a reason describing why it could not be started.
//
// On every failure path the server has already been stopped and reaped, so
// devServer.output() is safe to read afterwards; on the ready path the server
// is still running and the caller owns the stop.
func startDevServer(ctx context.Context, root, devCommand string, port int, timeout time.Duration) (*devServer, string) {
	if timeout <= 0 {
		timeout = DefaultDevServerTimeout
	}
	ds := &devServer{out: &bytes.Buffer{}}
	proc, err := StartDevProcess(root, devCommand, ds.out)
	if err != nil {
		return ds, "start dev server: " + err.Error()
	}
	ds.proc = proc
	ds.done = make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(ds.done)
	}()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(devServerPollInterval)
	defer ticker.Stop()
	for {
		if probeDevPort(port) {
			return ds, ""
		}
		select {
		case <-ds.done:
			// The dev command exited before the port came up.
			stopDevServer(ds)
			return ds, "the dev command exited before the dev port accepted a request"
		case <-ctx.Done():
			stopDevServer(ds)
			return ds, "verification run cancelled before the dev server became ready"
		case <-ticker.C:
			if time.Now().After(deadline) {
				stopDevServer(ds)
				return ds, "the dev server did not become ready within " + timeout.String()
			}
		}
	}
}

// stopDevServer stops the dev server's process group and waits for it to be
// reaped. It is safe to call on a server that was never started, already
// stopped, or still running: the stop is idempotent (a second kill on a dead
// group is a no-op, and <-done on a closed channel returns immediately).
func stopDevServer(ds *devServer) {
	if ds == nil || ds.proc == nil {
		return
	}
	ds.proc.Stop()
	if ds.done != nil {
		<-ds.done
	}
}

// output returns the dev server's captured combined output. It is safe to read
// only after the process has been reaped (i.e. after stopDevServer on a
// failure path); on the ready path the server is still writing.
func (ds *devServer) output() string {
	if ds == nil || ds.out == nil {
		return ""
	}
	return ds.out.String()
}

// probeDevPort reports whether anything on the dev port answers a plain HTTP
// GET. Any status code (including 404) means the server is up. It dials
// "localhost", not 127.0.0.1: dev servers such as Astro and Vite on recent
// Node bind only the IPv6 loopback (::1), and the resolver tries both.
func probeDevPort(port int) bool {
	client := &http.Client{Timeout: devProbeClientTimeout}
	resp, err := client.Get(devServerURL(port, "/"))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// devServerURL is the URL of path on the local dev server. The host is
// "localhost" so servers bound to either loopback family (127.0.0.1 or ::1)
// are reachable from the probe, the page check and the headless browser.
func devServerURL(port int, path string) string {
	return fmt.Sprintf("http://localhost:%d%s", port, path)
}
