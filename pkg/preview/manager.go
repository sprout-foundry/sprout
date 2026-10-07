// Package preview is the preview backend: it starts or detects the project's
// dev server from the starter manifest's dev command and port (loaded through
// pkg/starterstore), and tracks the server's lifecycle — starting, running,
// stopped, failed — for the preview pane's presentational component.
//
// The Manager is the single owner of a dev-server start for one project root,
// and it follows the starter manifest strictly:
//
//   - it never guesses a command. A missing manifest, or a manifest without a usable dev
//     declaration (a dev command AND a dev port), settles the state to stopped with a reason;
//   - it detects an already-running server on the manifest's dev port and adopts it (running,
//     detected) instead of starting a second one;
//   - a dev server it starts runs in its own process group (verify's DevProcess, the shared
//     platform process type), so Stop releases the port for real, and a liveness monitor
//     settles the state to failed when the dev command dies or the port stops answering.
//
// The package is standard-library plus repo-internal only, so it compiles for the CLI, the webui
// server, and WASM builds alike. Under WASM the process support is a no-op stub (verify's js
// build), so Start settles to failed with the stub's reason; hosted previews are out of scope
// here and are not a local dev server.
package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// readinessPollInterval is the interval between dev-port readiness probes while
// a spawned dev server is starting (verify's poll cadence).
const readinessPollInterval = 250 * time.Millisecond

// livenessPollInterval is the interval between dev-port probes while a spawned
// dev server is running, so a wedged or crashed server settles the state
// instead of showing a dead "running" pane.
const livenessPollInterval = 2 * time.Second

// Manager drives one project's dev server. One manager per project root; every
// method is safe for concurrent use.
type Manager struct {
	root         string
	readyTimeout time.Duration

	mu sync.Mutex
	// The last dev declaration Start resolved for this project.
	manifest   *startermanifest.StarterManifest
	port       int
	devCommand string

	// The owned dev server: the process this manager spawned (nil when the
	// running server was detected, or nothing is running), its captured
	// output, the reaper's signal, and the monitor generation that pins a
	// liveness monitor to the start cycle it was spawned for.
	proc       *verify.DevProcess
	out        *bytes.Buffer
	reaped     chan struct{}
	monitorGen int
	// startSettled is closed (and re-nil'd, under the lock) when the
	// in-flight start cycle settles; a concurrent Start waits on it
	// rather than spawning a second server.
	startSettled chan struct{}

	status   Status
	url      string
	err      string
	detected bool
	// stopping marks a Stop request in flight so a settle (a late exit of
	// the dev command) records stopped rather than failed.
	stopping bool
}

// State returns the dev server's current lifecycle state.
//
// A detected (external) running server is re-probed on every read: an
// external server that dies later settles to stopped (the manager did not
// start it, so "failed" would be a lie). A started (owned) server's
// liveness is maintained by the liveness monitor instead, so State never
// blocks on a probe in that case.
func (m *Manager) State() State {
	m.mu.Lock()
	if m.status == StatusRunning && m.detected {
		port := m.port
		m.mu.Unlock()
		up := probeHTTP(port)
		m.mu.Lock()
		if m.status == StatusRunning && m.detected && !up {
			m.status = StatusStopped
			m.url = ""
			m.detected = false
			m.err = fmt.Sprintf("the dev server detected on port %d is no longer responding", port)
		}
	}
	s := m.stateLocked()
	m.mu.Unlock()
	return s
}

// Start ensures the project's dev server is running and returns the state
// it settled into:
//
//   - running (detected) when something already answers on the manifest's
//     dev port — the manager adopts it instead of starting a second one;
//   - running when it spawned the manifest's dev command and the port
//     answered within the ready timeout;
//   - failed with a reason when the dev command could not be started,
//     exited before readiness, or the ready timeout elapsed;
//   - stopped with a reason when the project has no usable dev
//     declaration (missing or invalid manifest, no dev command, no dev
//     port) — the manager never guesses a command.
//
// Start is synchronous: it blocks until the outcome is known. Callers that
// want a non-blocking start (the webui handler, so the pane can poll State)
// run it in a goroutine. A Start that arrives while a first start is still
// settling waits for that cycle and reports its settled state — it never
// spawns a second server.
func (m *Manager) Start(ctx context.Context) State {
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.status == StatusStarting {
		// A start is in flight: wait for it to settle (or for the caller's
		// context), then report the outcome — never spawn a second server.
		return m.waitForInFlightSettleLocked(ctx)
	}
	switch m.status {
	case StatusRunning:
		port := m.port
		m.mu.Unlock()
		if probeHTTP(port) {
			return m.State()
		}
		// The port died out from under the running state: settle it, then
		// fall through to a fresh start.
		m.settlePortDead(port)
	default:
		m.mu.Unlock()
	}

	// No server is (known to be) running. Resolve the project's dev
	// declaration; a structural absence settles stopped, never guessed.
	manifest, merr := starterstore.LoadStarterManifest(m.root)

	devPort := 0
	usable := false
	var unavailable string
	switch {
	case errors.Is(merr, starterstore.ErrNoManifest):
		unavailable = "no starter manifest: the project declares no dev server"
	case merr != nil:
		unavailable = "the starter manifest is invalid: " + merr.Error()
	case manifest.Dev == "":
		unavailable = "the starter manifest declares no dev command (dev)"
	case manifest.DevPort == 0:
		unavailable = "the starter manifest declares no dev port (dev_port)"
	default:
		devPort = manifest.DevPort
		usable = true
	}

	if !usable {
		m.mu.Lock()
		if m.status == StatusStarting {
			return m.waitForInFlightSettleLocked(ctx)
		}
		m.markStoppedLocked(unavailable)
		st := m.stateLocked()
		m.mu.Unlock()
		return st
	}

	// Detect: something already answering on the manifest's dev port is
	// the dev server — adopt it instead of starting a second one.
	if probeHTTP(devPort) {
		m.mu.Lock()
		if m.status == StatusStarting {
			return m.waitForInFlightSettleLocked(ctx)
		}
		m.markDetectedLocked(manifest)
		st := m.stateLocked()
		m.mu.Unlock()
		return st
	}

	return m.startOwned(manifest, ctx)
}

// waitForInFlightSettleLocked waits, for a caller that holds m.mu and has
// observed an in-flight start cycle, for that cycle to settle (or for ctx
// to finish), then returns the settled state. It returns with the lock
// released, so the caller must not touch manager state after it.
func (m *Manager) waitForInFlightSettleLocked(ctx context.Context) State {
	settled := m.startSettled
	m.mu.Unlock()
	if settled != nil {
		select {
		case <-settled:
		case <-ctx.Done():
		}
	}
	return m.State()
}

// Restart stops the dev server this manager started and starts it again.
// A detected (external) server is not owned, so a restart re-detects it:
// the port already serves the app, and the outcome is running (detected).
func (m *Manager) Restart(ctx context.Context) State {
	m.Stop()
	return m.Start(ctx)
}

// Stop stops the dev server this manager started, releasing the port.
// It is idempotent: stopping an absent or already-reaped server is a
// no-op. A detected (external) server is not owned, so Stop leaves it
// alone and reports its (still running) state.
func (m *Manager) Stop() State {
	m.mu.Lock()
	if m.proc == nil && m.status == StatusRunning && m.detected {
		// An external server: the manager cannot stop it.
		st := m.stateLocked()
		m.mu.Unlock()
		return st
	}
	if m.proc != nil {
		m.stopping = true
		m.monitorGen++
		proc := m.proc
		reaped := m.reaped
		m.proc = nil
		m.mu.Unlock()
		proc.Stop()
		<-reaped
		m.mu.Lock()
	}
	m.status = StatusStopped
	m.url = ""
	m.err = ""
	m.detected = false
	m.stopping = false
	st := m.stateLocked()
	m.mu.Unlock()
	return st
}

// startOwned spawns the manifest's dev command, then waits (synchronously,
// on the calling goroutine) for the dev port to answer or the ready
// timeout to elapse, settling the state: running, failed, or stopped (a
// cancelled context or a concurrent Stop). The caller that wants the start
// not to block (the webui handler) runs Start in a goroutine.
func (m *Manager) startOwned(manifest *startermanifest.StarterManifest, ctx context.Context) State {
	m.mu.Lock()
	if m.status == StatusStarting {
		return m.waitForInFlightSettleLocked(ctx)
	}
	m.manifest = manifest
	m.port = manifest.DevPort
	m.devCommand = manifest.Dev
	m.out = &bytes.Buffer{}
	m.stopping = false
	m.status = StatusStarting
	m.url = ""
	m.err = ""
	m.detected = false
	m.startSettled = make(chan struct{})

	proc, err := verify.StartDevProcess(m.root, manifest.Dev, m.out)
	if err != nil {
		m.status = StatusFailed
		m.err = "start dev server: " + err.Error()
		m.proc = nil
		m.settleStartLocked()
		st := m.stateLocked()
		m.mu.Unlock()
		return st
	}
	m.proc = proc
	reaped := make(chan struct{})
	m.reaped = reaped
	m.monitorGen++
	go func() {
		_ = proc.Wait()
		close(reaped)
	}()
	port := m.port
	gen := m.monitorGen
	m.mu.Unlock()

	m.waitReady(ctx, port, reaped, gen)
	return m.State()
}

// waitReady polls the dev port until it answers or the ready timeout
// elapses, then settles the starting state: running (the port answers —
// the dev server owns it, and the liveness monitor takes over), failed
// (the dev command exited early, or the timeout passed — the process group
// is killed either way), or stopped (the caller's context cancelled, or a
// Stop arrived). It always settles the start cycle (closes startSettled).
func (m *Manager) waitReady(ctx context.Context, port int, reaped chan struct{}, gen int) {
	deadline := time.Now().Add(m.readyTimeout)
	ticker := time.NewTicker(readinessPollInterval)
	defer ticker.Stop()
	for {
		if probeHTTP(port) {
			m.mu.Lock()
			if m.status == StatusStarting && m.proc != nil && m.monitorGen == gen {
				m.status = StatusRunning
				m.url = devURL(port)
				m.err = ""
				go m.monitor(gen, m.reaped)
			}
			m.settleStartLocked()
			m.mu.Unlock()
			return
		}
		select {
		case <-reaped:
			m.mu.Lock()
			if m.status == StatusStarting && m.monitorGen == gen {
				m.url = ""
				if m.stopping {
					m.status = StatusStopped
				} else {
					m.status = StatusFailed
					m.err = "the dev command exited before the dev port accepted a request" + outputExcerpt(m.out)
				}
			}
			m.settleStartLocked()
			m.mu.Unlock()
			return
		case <-ctx.Done():
			m.mu.Lock()
			if m.proc != nil {
				m.stopping = true
				m.proc.Stop()
			}
			if m.status == StatusStarting && m.monitorGen == gen {
				m.status = StatusStopped
				m.url = ""
				m.err = "the dev server start was cancelled before readiness"
			}
			m.settleStartLocked()
			m.mu.Unlock()
			return
		case <-ticker.C:
			if time.Now().After(deadline) {
				m.mu.Lock()
				userStop := m.stopping
				if m.proc != nil {
					m.stopping = true
					m.proc.Stop()
				}
				if m.status == StatusStarting && m.monitorGen == gen {
					m.url = ""
					if userStop {
						m.status = StatusStopped
					} else {
						m.status = StatusFailed
						m.err = fmt.Sprintf("the dev server did not become ready within %s (dev port %d)", m.readyTimeout, port)
					}
				}
				m.settleStartLocked()
				m.mu.Unlock()
				return
			}
		}
	}
}

// settleStartLocked marks the in-flight start cycle settled: it closes
// startSettled (so a concurrent Start can proceed) and re-nils it, so it
// is closed exactly once. The caller holds m.mu.
func (m *Manager) settleStartLocked() {
	if m.startSettled != nil {
		close(m.startSettled)
		m.startSettled = nil
	}
}

// monitor keeps the running state honest for a dev server this manager
// started: it settles the state to failed when the dev command exits, or
// when the dev port stops answering (a wedged server). gen pins it to the
// start cycle it was spawned for, so a stale monitor (after a stop or a
// restart) never settles a newer cycle's state; reaped is that cycle's
// reaper channel (the manager replaces it per start, so it is captured,
// not re-read).
func (m *Manager) monitor(gen int, reaped chan struct{}) {
	ticker := time.NewTicker(livenessPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-reaped:
			m.settleCrash(gen, "the dev command exited while the preview was running")
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		port := m.port
		active := m.status == StatusRunning && m.proc != nil && m.monitorGen == gen && !m.stopping
		m.mu.Unlock()
		if !active {
			return
		}
		if probeHTTP(port) {
			continue
		}
		m.settleCrash(gen, fmt.Sprintf("the dev server stopped responding on port %d", port))
		return
	}
}

// settleCrash records a crashed dev server; no-op when the state already
// moved on (a stop or a restart) or the cycle is stale.
func (m *Manager) settleCrash(gen int, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status != StatusRunning || m.proc == nil || m.monitorGen != gen {
		return
	}
	m.status = StatusFailed
	m.url = ""
	m.err = reason
}

// settlePortDead records that a running owned dev server's port stopped
// answering; no-op when the state moved on (a stop already settled it) or
// the server was detected (State handles that transition).
func (m *Manager) settlePortDead(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status != StatusRunning || m.detected {
		return
	}
	m.status = StatusFailed
	m.url = ""
	m.err = fmt.Sprintf("the dev server stopped responding on port %d", port)
}

// stateLocked renders the current state; the caller holds m.mu. When the
// project structurally has no dev server (a stopped state), the reason is
// attached so a /api/preview/status consumer knows why.
func (m *Manager) stateLocked() State {
	s := State{Status: m.status, URL: m.url, Error: m.err, Detected: m.detected}
	if s.Status == StatusStopped && s.Error == "" {
		s.Error = m.unavailableReasonLocked()
	}
	return s
}

// unavailableReasonLocked explains a stopped state for a project with no
// usable dev declaration; "" when a dev server simply is not running yet
// (or was stopped). The caller holds m.mu.
func (m *Manager) unavailableReasonLocked() string {
	manifest, err := starterstore.LoadStarterManifest(m.root)
	if errors.Is(err, starterstore.ErrNoManifest) {
		return "no starter manifest: the project declares no dev server"
	}
	if err != nil {
		return "the starter manifest is invalid: " + err.Error()
	}
	if manifest.Dev == "" {
		return "the starter manifest declares no dev command (dev)"
	}
	if manifest.DevPort == 0 {
		return "the starter manifest declares no dev port (dev_port)"
	}
	return ""
}

// markStoppedLocked records a stopped state with a reason; the caller
// holds m.mu.
func (m *Manager) markStoppedLocked(reason string) {
	m.status = StatusStopped
	m.url = ""
	m.err = reason
	m.detected = false
}

// markDetectedLocked adopts an already-running server found on the
// manifest's dev port; the caller holds m.mu.
func (m *Manager) markDetectedLocked(manifest *startermanifest.StarterManifest) {
	m.manifest = manifest
	m.port = manifest.DevPort
	m.devCommand = manifest.Dev
	m.proc = nil
	m.reaped = nil
	m.monitorGen++
	m.status = StatusRunning
	m.url = devURL(manifest.DevPort)
	m.err = ""
	m.detected = true
}
