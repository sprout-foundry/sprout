//go:build !js

// Package daemon — Phase 2: Daemon lifecycle + auto-start machinery.
//
// The functions in this file (DaemonSpec, DetectDaemon, StartDaemon,
// EnsureDaemon) provide the orchestration logic to detect an existing
// daemon, spawn one if needed (with cross-process flock election), and
// return control to the caller.  The DaemonLifecycle type in lifecycle.go
// tracks active connections and reaps idle daemons.
//
// Package design: pkg/daemon is intentionally lean — it depends only on
// stdlib, github.com/gofrs/flock, and pkg/envutil (stdlib-only env helper).
// No import of pkg/webui, pkg/agent, or pkg/agent_tools.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// Default daemon port. Matches webui.DaemonPort; we hardcode it here to
// avoid importing pkg/webui (heavy dependency) into pkg/daemon.
const defaultDaemonPort = 56000

// Default timeouts and paths used when no overrides are supplied.
const (
	defaultStartTimeout  = 10 * time.Second
	defaultShutdownDelay = 60 * time.Second
)

// ErrDaemonDisabled is returned by EnsureDaemon when SPROUT_DAEMON=0.
// Callers should treat this as "daemon disabled" and fall back to
// in-process execution.
var ErrDaemonDisabled = errors.New("daemon disabled via SPROUT_DAEMON=0")

// ---------------------------------------------------------------------------
// DaemonSpec
// ---------------------------------------------------------------------------

// DaemonSpec configures the daemon lifecycle (URL, socket, PID file,
// spawn command, and timeouts). Zero-value fields are replaced with
// defaults by DefaultDaemonSpec(); callers may override any field.
type DaemonSpec struct {
	// DaemonURL is the HTTP base URL to reach the daemon (e.g.
	// "http://127.0.0.1:56000").
	DaemonURL string

	// SocketPath is the Unix-domain socket path as an alternative to
	// TCP. If set, DetectDaemon tries this socket when DaemonURL fails.
	SocketPath string

	// PIDFilePath is the path to the PID-file lock used for cross-process
	// election. A flock on this file ensures exactly one process spawns
	// the daemon.
	PIDFilePath string

	// StartTimeout limits how long StartDaemon and EnsureDaemon wait for
	// the daemon to become healthy after spawning.
	StartTimeout time.Duration

	// ShutdownDelay is the idle timeout used by DaemonLifecycle: when
	// the last connection disconnects, the daemon is stopped after this
	// delay unless a new connection arrives.
	ShutdownDelay time.Duration

	// DaemonCommand is the command (args[0] is the binary) used to spawn
	// the daemon process. Default: [executable-path, "agent", "-d"].
	DaemonCommand []string

	// Env holds extra KEY=VALUE environment variables applied to the
	// spawned daemon process (merged over the parent's environment).
	// Used e.g. to set SPROUT_DAEMON_IDLE_TIMEOUT so auto-started
	// daemons reap themselves after an idle period.
	Env []string

	// LogPath is where the spawned daemon's stdout/stderr are redirected.
	// If empty, output goes to os.DevNull.
	LogPath string
}

// DefaultDaemonSpec returns a DaemonSpec with all fields set to their
// conventional defaults. Callers may then override individual fields
// (e.g. SocketPath for test isolation).
func DefaultDaemonSpec() DaemonSpec {
	execPath, err := os.Executable()
	if err != nil {
		execPath = "sprout"
	}

	dataDir, err := envutil.DataDir()
	if err != nil {
		dataDir = filepath.Join(os.TempDir(), "sprout")
	}

	return DaemonSpec{
		DaemonURL:     fmt.Sprintf("http://127.0.0.1:%d", defaultDaemonPort),
		SocketPath:    filepath.Join(dataDir, "daemon.sock"),
		PIDFilePath:   filepath.Join(dataDir, "daemon.pid"),
		StartTimeout:  defaultStartTimeout,
		ShutdownDelay: defaultShutdownDelay,
		DaemonCommand: []string{execPath, "agent", "-d"},
	}
}

// ---------------------------------------------------------------------------
// DetectDaemon
// ---------------------------------------------------------------------------

// DetectDaemon checks whether a healthy daemon is already reachable.
//
// It tries GET on DaemonURL first (HTTP TCP). If that fails and SocketPath
// is non-empty, it tries a Unix-domain socket connection. Healthy means
// HTTP 200 from the /health endpoint (matching the check in health.go).
func DetectDaemon(ctx context.Context, spec DaemonSpec) (bool, error) {
	// Try TCP DaemonURL.
	if ok, _ := detectHTTP(ctx, spec.DaemonURL, 2*time.Second); ok {
		return true, nil
	}

	// Fallback: Unix socket if configured.
	if spec.SocketPath != "" {
		if ok, _ := detectUnixSocket(ctx, spec.SocketPath, 2*time.Second); ok {
			return true, nil
		}
	}

	// Neither endpoint is healthy. No error — the daemon just isn't running.
	return false, nil
}

// detectHTTP sends a GET to baseURL+"/health" and reports whether it
// returns HTTP 200. Uses a fresh connection (no keep-alive pooling) so a
// daemon that has exited is never masked by a stale pooled connection.
func detectHTTP(ctx context.Context, baseURL string, timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return false, fmt.Errorf("create health request: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, nil // network error → daemon not available (not a real error)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	return true, nil
}

// detectUnixSocket connects to a Unix socket and performs an HTTP GET
// on "/health". The URL scheme "http://unix" is a convention: the host
// is ignored by the dialer, which connects to socketPath.
func detectUnixSocket(ctx context.Context, socketPath string, timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/health", nil)
	if err != nil {
		return false, fmt.Errorf("create unix health request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// StartDaemon
// ---------------------------------------------------------------------------

// StartDaemon spawns the daemon process (detached) and polls until it
// is healthy or StartTimeout elapses. The spawned daemon is NOT tied to
// the caller's context lifetime — it survives ctx cancellation and the
// caller exiting. Returns an error on timeout, ctx cancellation, or if
// the process fails to start.
func StartDaemon(ctx context.Context, spec DaemonSpec) error {
	return startDaemonInner(ctx, spec, slog.Default())
}

func startDaemonInner(ctx context.Context, spec DaemonSpec, logger *slog.Logger) error {
	// Use exec.Command (NOT CommandContext) so the spawned daemon is NOT
	// tied to the caller's context lifetime. The daemon must survive the
	// CLI exiting and be reaped later by the idle timer.
	cmd := exec.Command(spec.DaemonCommand[0], spec.DaemonCommand[1:]...)

	// Merge extra environment variables (e.g. SPROUT_DAEMON_IDLE_TIMEOUT)
	// over the parent's environment.
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}

	// Detach from parent session so the daemon survives the caller exiting.
	applyDetach(cmd)

	// Redirect stdio.
	if spec.LogPath != "" {
		f, err := os.OpenFile(spec.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return fmt.Errorf("open daemon log %s: %w", spec.LogPath, err)
		}
		cmd.Stdout = f
		cmd.Stderr = f
	} else {
		if err := ensureStdioDevNull(cmd); err != nil {
			return fmt.Errorf("setup daemon stdio: %w", err)
		}
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Poll DetectDaemon until healthy, ctx cancelled, or timeout.
	// Context cancellation returns an error but does NOT kill the
	// detached daemon — it may still come up on its own.
	deadline := time.Now().Add(spec.StartTimeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		// Non-blocking deadline check — distinct from ctx cancellation.
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon did not become healthy within %s", spec.StartTimeout)
		}

		healthy, _ := DetectDaemon(ctx, spec)
		if healthy {
			logger.Info("daemon started and healthy", slog.String("url", spec.DaemonURL))
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("start cancelled: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// ---------------------------------------------------------------------------
// EnsureDaemon
// ---------------------------------------------------------------------------

// EnsureDaemon ensures a healthy daemon is running. It uses the PID file
// as a flock-based election primitive: exactly one process spawns the
// daemon; competing processes wait for it to become healthy.
//
// Election protocol (single-shot TryLock):
//   - Winner (TryLock == true): writes PID, spawns daemon via startDaemonInner,
//     polls health, then releases the flock after the daemon is healthy.
//   - Loser (TryLock == false): does NOT call Unlock (never held the lock).
//     Polls DetectDaemon every ~300ms up to StartTimeout. If the daemon
//     becomes healthy → returns (false, nil). On timeout or ctx cancellation
//     → returns an error.
//
// The escape hatch: if SPROUT_DAEMON env is "0", returns ErrDaemonDisabled.
//
// Returns (alreadyRunning=true, nil) if a daemon was already reachable
// before this call.  Returns (alreadyRunning=false, nil) if this call
// started the daemon and it became healthy, or if another process started
// it and this call only waited for it.
//
// The flock guards the election only, not the daemon's lifetime.  A stale
// PID file from a crashed process is handled automatically because the OS
// releases the flock when the process exits.
func EnsureDaemon(ctx context.Context, spec DaemonSpec) (alreadyRunning bool, err error) {
	return ensureDaemonInner(ctx, spec, slog.Default())
}

func ensureDaemonInner(ctx context.Context, spec DaemonSpec, logger *slog.Logger) (bool, error) {
	// Escape hatch: SPROUT_DAEMON=0 → caller falls back to in-process.
	if v, ok := envutil.LookupEnv("DAEMON"); ok && v == "0" {
		return false, ErrDaemonDisabled
	}

	// Fast path: daemon already reachable → just connect.
	already, _ := DetectDaemon(ctx, spec)
	if already {
		logger.Info("daemon already running", slog.String("url", spec.DaemonURL))
		return true, nil
	}

	// Ensure the PID-file directory exists.
	pidDir := filepath.Dir(spec.PIDFilePath)
	if err := os.MkdirAll(pidDir, 0700); err != nil {
		return false, fmt.Errorf("create PID-file directory %s: %w", pidDir, err)
	}

	// Single-shot flock election.
	// TryLock attempts ONCE and returns immediately.
	//   ok==true  → we are the winner (leader)
	//   ok==false → another process holds the lock (loser path)
	// This is NOT TryLockContext which retries until it wins or context
	// expires — that would let a late-arriving process steal the lock
	// after the winner releases it and spawn a second daemon.
	f := flock.New(spec.PIDFilePath)
	ok, err := f.TryLock()
	if err != nil {
		return false, fmt.Errorf("acquire PID-file lock on %s: %w", spec.PIDFilePath, err)
	}

	if !ok {
		// LOSER path: another process holds the lock and is spawning.
		// We never held the lock → do NOT call Unlock.
		logger.Info("another process holds the lock, waiting for health",
			slog.String("url", spec.DaemonURL))

		// Poll until the daemon becomes healthy, ctx is cancelled, or timeout.
		deadline := time.Now().Add(spec.StartTimeout)
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()

		for {
			if time.Now().After(deadline) {
				return false, fmt.Errorf("daemon did not become healthy within %s (waiting on another process)", spec.StartTimeout)
			}

			healthy, _ := DetectDaemon(ctx, spec)
			if healthy {
				logger.Info("daemon became healthy (spawned by another process)",
					slog.String("url", spec.DaemonURL))
				return false, nil
			}

			select {
			case <-ctx.Done():
				return false, fmt.Errorf("wait cancelled: %w", ctx.Err())
			case <-ticker.C:
			}
		}
	}

	// WINNER path: we hold the lock — we're the leader.
	// Write our PID so crash analysis and diagnostics can identify us.
	if err := os.WriteFile(spec.PIDFilePath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		logger.Warn("failed to write PID file", slog.Any("err", err))
	}

	defer f.Unlock() // Release after daemon is healthy.

	logger.Info("starting daemon (elected leader)", slog.String("url", spec.DaemonURL))
	if err := startDaemonInner(ctx, spec, logger); err != nil {
		return false, err
	}

	return false, nil
}
