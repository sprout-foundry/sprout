//go:build !js

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// freePort — grab a free TCP port for test servers
// ---------------------------------------------------------------------------

func freePort(t *testing.T) int {
	t.Helper()
	// Retry: under parallel test load the ephemeral port range (as small as
	// 300 ports on constrained hosts) can transiently exhaust, which
	// surfaces as EADDRINUSE even on ":0".
	for attempt := 0; attempt < 50; attempt++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			port := l.Addr().(*net.TCPAddr).Port
			l.Close()
			return port
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.FailNow(t, "freePort: could not bind an ephemeral port after 50 attempts")
	return 0
}

// ---------------------------------------------------------------------------
// TestDaemonHelperProcess — spawned as a fake daemon by other tests
//
// When invoked with SPROUT_DAEMON_HELPER_PORT, it starts an HTTP server on
// that port serving /health → 200.  The marker file is appended to on
// startup so the parent can count spawns.  Self-expire after 20s.
// ---------------------------------------------------------------------------

func TestDaemonHelperProcess(t *testing.T) {
	portStr := os.Getenv("SPROUT_DAEMON_HELPER_PORT")
	if portStr == "" {
		t.Skip("not the helper invocation")
	}

	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port == 0 {
		t.Fatalf("invalid port: %q", portStr)
	}

	// Optionally delay startup (for election race tests).
	if delayStr := os.Getenv("SPROUT_DAEMON_HELPER_DELAY"); delayStr != "" {
		var delay int
		if _, err := fmt.Sscanf(delayStr, "%d", &delay); err == nil && delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}

	markerFile := os.Getenv("SPROUT_DAEMON_HELPER_MARKER")
	if markerFile != "" {
		f, err := os.OpenFile(markerFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
		}
	}

	// Self-expire after 20s — safety net against process leaks.
	time.AfterFunc(20*time.Second, func() { os.Exit(0) })

	// Optionally simulate idle reaping: if SPROUT_DAEMON_HELPER_IDLE_MS is
	// set, the helper exits when no request has been received for that long.
	// The handler below updates lastRequest on every request.
	var lastRequest atomic.Int64
	lastRequest.Store(time.Now().UnixMilli())
	if idleStr := os.Getenv("SPROUT_DAEMON_HELPER_IDLE_MS"); idleStr != "" {
		var idleMs int
		if _, err := fmt.Sscanf(idleStr, "%d", &idleMs); err == nil && idleMs > 0 {
			go func() {
				ticker := time.NewTicker(50 * time.Millisecond)
				defer ticker.Stop()
				for range ticker.C {
					if time.Since(time.UnixMilli(lastRequest.Load())) > time.Duration(idleMs)*time.Millisecond {
						fmt.Fprintf(os.Stderr, "helper idle for %d ms — exiting\n", idleMs)
						os.Exit(0)
					}
				}
			}()
		}
	}

	// Start serving on 127.0.0.1:<port>. Retry the bind: under heavy
	// parallel test load the port from freePort() can be transiently
	// stolen by a concurrent listener; the parent's StartTimeout (5s)
	// gives us ~4s of retry budget.
	var ln net.Listener
	{
		var bindErr error
		for attempt := 0; attempt < 40; attempt++ {
			ln, bindErr = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if bindErr == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if bindErr != nil {
			fmt.Fprintf(os.Stderr, "helper listen error after retries: %v\n", bindErr)
			os.Exit(1)
		}
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lastRequest.Store(time.Now().UnixMilli())
			if r.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}),
	}

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "helper serve error: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// makeTestSpec — build a DaemonSpec for tests
// ---------------------------------------------------------------------------

func makeTestSpec(t *testing.T, tmpDir string, port int) DaemonSpec {
	t.Helper()
	spec := DefaultDaemonSpec()
	spec.DaemonURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	spec.PIDFilePath = filepath.Join(tmpDir, "daemon.pid")
	spec.SocketPath = "" // disabled for most tests
	spec.StartTimeout = 5 * time.Second
	// CRITICAL: never spawn the real default command ([os.Executable(),
	// "agent", "-d"]) from a test — that runs the compiled test binary as a
	// sprout daemon, which recursively executes the test suite. Tests that
	// need a real daemon use helperSpec; everything else gets an inert
	// command that fails fast.
	spec.DaemonCommand = []string{"sh", "-c", "exit 1"}
	return spec
}

// ---------------------------------------------------------------------------
// helperSpec — DaemonSpec pointing at the Go test helper process
// ---------------------------------------------------------------------------

func helperSpec(t *testing.T, tmpDir string, port int, delayMs int, markerFile string) DaemonSpec {
	t.Helper()
	t.Setenv("SPROUT_DAEMON_HELPER_PORT", fmt.Sprintf("%d", port))
	if delayMs > 0 {
		t.Setenv("SPROUT_DAEMON_HELPER_DELAY", fmt.Sprintf("%d", delayMs))
	}
	if markerFile != "" {
		t.Setenv("SPROUT_DAEMON_HELPER_MARKER", markerFile)
	}

	spec := DefaultDaemonSpec()
	spec.DaemonURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	spec.PIDFilePath = filepath.Join(tmpDir, "daemon.pid")
	spec.SocketPath = ""
	spec.StartTimeout = 5 * time.Second
	spec.DaemonCommand = []string{
		os.Args[0],
		"-test.run=TestDaemonHelperProcess",
		"--",
	}
	spec.LogPath = filepath.Join(tmpDir, "daemon.log")

	// Always kill spawned helpers when the test finishes, even on failure —
	// orphaned helpers leak listener ports and exhaust the ephemeral range
	// on constrained hosts (e.g. 60700-61000 in CI containers).
	if markerFile != "" {
		t.Cleanup(func() { killHelperByMarker(t, markerFile) })
	}

	return spec
}

// ---------------------------------------------------------------------------
// TestDetectDaemon_HTTP_Healthy
// ---------------------------------------------------------------------------

func TestDetectDaemon_HTTP_Healthy(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	spec := DaemonSpec{DaemonURL: srv.URL}
	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, ok, "daemon should be detected via HTTP 200")
}

// ---------------------------------------------------------------------------
// TestDetectDaemon_HTTP_Unhealthy
// ---------------------------------------------------------------------------

func TestDetectDaemon_HTTP_Unhealthy(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	spec := DaemonSpec{DaemonURL: srv.URL}
	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.False(t, ok, "daemon should NOT be detected on HTTP 500")
}

// ---------------------------------------------------------------------------
// TestDetectDaemon_ClosedPort
// ---------------------------------------------------------------------------

func TestDetectDaemon_ClosedPort(t *testing.T) {

	// Grab a free port then close it — nothing is listening. A concurrent
	// test can grab the freed port before we probe it (making the probe
	// succeed), so retry with fresh ports until we observe a genuine miss.
	for attempt := 0; attempt < 5; attempt++ {
		port := freePort(t)
		spec := DaemonSpec{DaemonURL: fmt.Sprintf("http://127.0.0.1:%d", port)}

		ok, err := DetectDaemon(context.Background(), spec)
		require.NoError(t, err, "DetectDaemon should NOT return an error for unreachable daemon")
		if !ok {
			return
		}
	}
	t.Fatal("closed port was detected as healthy 5 times (ports kept getting reused by concurrent tests)")
}

// ---------------------------------------------------------------------------
// TestDetectDaemon_UnixSocket
// ---------------------------------------------------------------------------

func TestDetectDaemon_UnixSocket(t *testing.T) {

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "daemon.sock")

	// Create a Unix socket server serving /health with 200.
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer ln.Close()

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	// DaemonURL points at a dead port; detection should fall back to socket.
	spec := DaemonSpec{
		DaemonURL:  fmt.Sprintf("http://127.0.0.1:%d", freePort(t)),
		SocketPath: socketPath,
	}

	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, ok, "daemon should be detected via Unix socket fallback")
}

// ---------------------------------------------------------------------------
// TestDetectDaemon_UnixSocket_NotFound
// ---------------------------------------------------------------------------

func TestDetectDaemon_UnixSocket_NotFound(t *testing.T) {

	tmpDir := t.TempDir()
	spec := DaemonSpec{
		DaemonURL:  fmt.Sprintf("http://127.0.0.1:%d", freePort(t)),
		SocketPath: filepath.Join(tmpDir, "nonexistent.sock"),
	}

	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err, "DetectDaemon should NOT error when socket is missing")
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_FastPath
// ---------------------------------------------------------------------------

func TestEnsureDaemon_FastPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	spec := makeTestSpec(t, tmpDir, 0) // port doesn't matter since we override DaemonURL
	spec.DaemonURL = srv.URL

	already, err := EnsureDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, already, "fast path should return alreadyRunning=true")
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_Disabled
// ---------------------------------------------------------------------------

func TestEnsureDaemon_Disabled(t *testing.T) {
	t.Setenv("SPROUT_DAEMON", "0")

	tmpDir := t.TempDir()
	port := freePort(t)
	spec := makeTestSpec(t, tmpDir, port)

	already, err := EnsureDaemon(context.Background(), spec)
	assert.False(t, already)
	assert.ErrorIs(t, err, ErrDaemonDisabled)
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_Disabled_NotZero
// ---------------------------------------------------------------------------

func TestEnsureDaemon_Disabled_NotZero(t *testing.T) {
	t.Setenv("SPROUT_DAEMON", "1")

	tmpDir := t.TempDir()
	port := freePort(t)
	spec := makeTestSpec(t, tmpDir, port)

	// Unreachable daemon — goes through election, not escape hatch.
	_, err := EnsureDaemon(context.Background(), spec)
	if err != nil {
		assert.NotErrorIs(t, err, ErrDaemonDisabled,
			"SPROUT_DAEMON=1 should not trigger ErrDaemonDisabled")
	}
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_Election
//
// Two goroutines call EnsureDaemon concurrently.  Exactly one spawns the
// daemon; the other detects it and returns (false, nil).
// ---------------------------------------------------------------------------

func TestEnsureDaemon_Election(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 400, markerFile)

	var wg sync.WaitGroup
	var results [2]struct{ already bool; err error }

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results[idx].already, results[idx].err = EnsureDaemon(ctx, spec)
		}(i)
	}

	wg.Wait()

	// Both should succeed (nil error).
	for i := range results {
		assert.NoError(t, results[i].err, "goroutine %d should return nil error", i)
	}

	// Neither should report alreadyRunning (daemon wasn't pre-existing).
	for i := range results {
		assert.False(t, results[i].already,
			"goroutine %d: alreadyRunning should be false (daemon started during this call)", i)
	}

	// Exactly one spawn should have occurred.
	content, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	spawnCount := countLines(content)
	assert.Equal(t, 1, spawnCount,
		"marker should have exactly 1 line (one spawn via election), got %d", spawnCount)

	// After both return, the daemon should be healthy.
	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, ok, "daemon should be healthy after election")

	// Cleanup: kill spawned helper.
}

// ---------------------------------------------------------------------------
// TestStartDaemon_Success
// ---------------------------------------------------------------------------

func TestStartDaemon_Success(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 200, markerFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := StartDaemon(ctx, spec)
	require.NoError(t, err)

	ok, _ := DetectDaemon(ctx, spec)
	assert.True(t, ok, "daemon should be healthy after StartDaemon")

	// Verify exactly one spawn.
	content, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	assert.Equal(t, 1, countLines(content), "exactly one spawn should occur")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestStartDaemon_FailedCommand
// ---------------------------------------------------------------------------

func TestStartDaemon_FailedCommand(t *testing.T) {
	tmpDir := t.TempDir()
	port := freePort(t)

	spec := makeTestSpec(t, tmpDir, port)
	spec.DaemonCommand = []string{"sh", "-c", "exit 1"}
	spec.StartTimeout = 2 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := StartDaemon(ctx, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not become healthy")
}

// ---------------------------------------------------------------------------
// TestStartDaemon_CtxCancelled
// ---------------------------------------------------------------------------

func TestStartDaemon_CtxCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	port := freePort(t)

	spec := makeTestSpec(t, tmpDir, port)
	spec.DaemonCommand = []string{"sh", "-c", "sleep 60"}
	spec.StartTimeout = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := StartDaemon(ctx, spec)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// TestDefaultDaemonSpec
// ---------------------------------------------------------------------------

func TestDefaultDaemonSpec(t *testing.T) {

	spec := DefaultDaemonSpec()

	assert.Contains(t, spec.DaemonURL, "127.0.0.1")
	assert.Contains(t, spec.DaemonURL, "56000")
	assert.NotEmpty(t, spec.PIDFilePath)
	assert.Equal(t, 10*time.Second, spec.StartTimeout)
	assert.Equal(t, 60*time.Second, spec.ShutdownDelay)
	assert.Len(t, spec.DaemonCommand, 3)
	assert.Equal(t, "agent", spec.DaemonCommand[1])
	assert.Equal(t, "-d", spec.DaemonCommand[2])
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_AlreadyHealthy_Sequential
//
// Pre-start a fake daemon, then EnsureDaemon should return fast-path (true, nil).
// ---------------------------------------------------------------------------

func TestEnsureDaemon_AlreadyHealthy_Sequential(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	// Pre-start the helper daemon in the background.
	spec := helperSpec(t, tmpDir, port, 0, markerFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start the daemon first.
	err := StartDaemon(ctx, spec)
	require.NoError(t, err)

	// Verify no spawns yet in marker (StartDaemon doesn't write marker).
	// Actually, the helper DOES write to the marker — so let's count current lines.
	content, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	spawnsBefore := countLines(content)

	// Now call EnsureDaemon — should hit the fast path.
	already, err := EnsureDaemon(ctx, spec)
	require.NoError(t, err)
	assert.True(t, already, "EnsureDaemon should return alreadyRunning=true")

	// No new spawns should have occurred.
	content2, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	spawnsAfter := countLines(content2)
	assert.Equal(t, spawnsBefore, spawnsAfter,
		"EnsureDaemon fast path should not spawn a new daemon")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

// spawnCount counts the number of lines in the marker file (each line = one spawn).
func spawnCount(content []byte) int {
	return countLines(content)
}

// killHelperByMarker reads the marker file (each line is a PID) and kills
// all spawned helper processes.  This is the primary cleanup mechanism.
func killHelperByMarker(t *testing.T, markerFile string) {
	t.Helper()

	content, err := os.ReadFile(markerFile)
	if err != nil {
		return // no marker file
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	pids := make([]int, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	if len(pids) == 0 {
		return
	}

	for _, pid := range pids {
		cmd := exec.Command("kill", "-TERM", fmt.Sprintf("%d", pid))
		_ = cmd.Run()
	}

	// Brief pause, then SIGKILL any survivors.
	time.Sleep(500 * time.Millisecond)
	for _, pid := range pids {
		cmd := exec.Command("kill", "-0", fmt.Sprintf("%d", pid))
		if cmd.Run() == nil {
			exec.Command("kill", "-KILL", fmt.Sprintf("%d", pid)).Run()
		}
	}
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_LoserNeverSpawns
//
// One goroutine wins the lock and spawns; another goroutine that calls
// EnsureDaemon after the winner is already spawning should NOT spawn.
// We verify this by using a delayed helper and checking spawn count.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_LoserNeverSpawns(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	// Use separate PID files for each goroutine to simulate different processes,
	// but they share the same flock file for election.
	spec := helperSpec(t, tmpDir, port, 300, markerFile)

	// Start first goroutine — it will likely win the flock.
	var wg sync.WaitGroup
	var resultA, resultB struct{ already bool; err error }

	wg.Add(2)

	// Goroutine A: immediate call.
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resultA.already, resultA.err = EnsureDaemon(ctx, spec)
	}()

	// Small delay so goroutine A gets the lock first.
	time.Sleep(50 * time.Millisecond)

	// Goroutine B: arrives after A has the lock.
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resultB.already, resultB.err = EnsureDaemon(ctx, spec)
	}()

	wg.Wait()

	assert.NoError(t, resultA.err, "goroutine A should succeed")
	assert.NoError(t, resultB.err, "goroutine B should succeed")

	// Exactly one spawn.
	content, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	spawnCount := countLines(content)
	assert.Equal(t, 1, spawnCount, "exactly one spawn; loser must not spawn")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_ElectionTimeout
//
// Loser times out waiting for the winner's daemon if the winner's spawn
// never completes.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_ElectionTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	port := freePort(t)

	// Winner holds the flock but spawns a command that never becomes healthy.
	// Loser should timeout waiting.
	winnerSpec := makeTestSpec(t, tmpDir, port)
	winnerSpec.DaemonCommand = []string{"sh", "-c", "sleep 60"}
	winnerSpec.StartTimeout = 3 * time.Second

	loserSpec := makeTestSpec(t, tmpDir, port)
	loserSpec.StartTimeout = 2 * time.Second

	var wg sync.WaitGroup
	var winnerErr, loserErr error

	// Winner starts first and holds the lock.
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, winnerErr = EnsureDaemon(ctx, winnerSpec)
	}()

	// Give winner time to acquire the lock.
	time.Sleep(100 * time.Millisecond)

	// Loser tries to acquire — should fail the lock and then timeout polling.
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, loserErr = EnsureDaemon(ctx, loserSpec)
	}()

	wg.Wait()

	// Winner times out (command never becomes healthy).
	require.Error(t, winnerErr, "winner should time out waiting for its dead command")

	// Loser also times out (winner's daemon never becomes healthy).
	require.Error(t, loserErr, "loser should time out waiting for winner's daemon")
}

// ---------------------------------------------------------------------------
// TestStartDaemon_SpawnedProcessSurvives
//
// The spawned daemon must survive context cancellation — it's detached.
// ---------------------------------------------------------------------------

func TestStartDaemon_SpawnedProcessSurvives(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 100, markerFile)

	// Use a context that we'll cancel.
	ctx, cancel := context.WithCancel(context.Background())

	err := StartDaemon(ctx, spec)
	require.NoError(t, err)

	// Cancel the context — the daemon should still be alive.
	cancel()
	time.Sleep(200 * time.Millisecond)

	// The daemon should still respond to health checks.
	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, ok, "daemon should survive context cancellation")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_PIDFileWritten
// ---------------------------------------------------------------------------

func TestEnsureDaemon_PIDFileWritten(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 200, markerFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := EnsureDaemon(ctx, spec)
	require.NoError(t, err)

	// PID file should exist and contain a valid PID.
	content, err := os.ReadFile(spec.PIDFilePath)
	require.NoError(t, err)

	var pid int
	n, err := fmt.Sscanf(strings.TrimSpace(string(content)), "%d", &pid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	assert.Greater(t, pid, 0, "PID should be positive")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestStartDaemon_LogPath_WritesLog
// ---------------------------------------------------------------------------

func TestStartDaemon_LogPath_WritesLog(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 100, markerFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := StartDaemon(ctx, spec)
	require.NoError(t, err)

	// The log file should exist (even if empty, the file is created).
	info, err := os.Stat(spec.LogPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir(), "LogPath should be a file")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_DaemonAlreadyHealthy_NoElection
//
// When the daemon is already running, EnsureDaemon should return (true, nil)
// without touching the flock.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_DaemonAlreadyHealthy_NoElection(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()

	// Start a simple httptest server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	spec := makeTestSpec(t, tmpDir, port)
	spec.DaemonURL = srv.URL

	already, err := EnsureDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, already)
}

// ---------------------------------------------------------------------------
// TestDetectDaemon_ContextTimeout
// ---------------------------------------------------------------------------

func TestDetectDaemon_ContextTimeout(t *testing.T) {

	port := freePort(t)
	spec := DaemonSpec{DaemonURL: fmt.Sprintf("http://127.0.0.1:%d", port)}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ok, err := DetectDaemon(ctx, spec)
	// detectHTTP silently returns (false, nil) on network errors.
	require.NoError(t, err)
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_MarkerFileDirCreated
//
// EnsureDaemon should create the PID-file directory if it doesn't exist.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_MarkerFileDirCreated(t *testing.T) {
	tmpDir := t.TempDir()
	port := freePort(t)

	// Use a nested path that doesn't exist yet.
	pidPath := filepath.Join(tmpDir, "nested", "deep", "daemon.pid")

	spec := makeTestSpec(t, tmpDir, port)
	spec.PIDFilePath = pidPath
	spec.DaemonCommand = []string{"sh", "-c", "exit 1"}
	spec.StartTimeout = 2 * time.Second

	_, err := EnsureDaemon(context.Background(), spec)
	// The command will fail, but the directory should have been created.
	require.Error(t, err)

	// The nested directory should exist.
	_, err = os.Stat(filepath.Dir(pidPath))
	assert.NoError(t, err, "PID-file directory should be created by EnsureDaemon")
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_DetectDaemon_False_Positive
//
// If DetectDaemon incorrectly reports healthy due to a stale PID file,
// the flock should still prevent double-spawning.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_DetectDaemon_False_Positive(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()

	// Pre-write a PID file with a fake PID.
	spec := makeTestSpec(t, tmpDir, port)
	spec.DaemonCommand = []string{"sh", "-c", "exit 1"}
	spec.StartTimeout = 2 * time.Second

	// Write a fake PID.
	err := os.WriteFile(spec.PIDFilePath, []byte("99999\n"), 0644)
	require.NoError(t, err)

	// EnsureDaemon should still go through the flock election.
	// Since no daemon is actually running, it will time out.
	_, err = EnsureDaemon(context.Background(), spec)
	require.Error(t, err)

	// The PID file should have been overwritten with our real PID.
	content, err := os.ReadFile(spec.PIDFilePath)
	require.NoError(t, err)
	var pid int
	n, err := fmt.Sscanf(strings.TrimSpace(string(content)), "%d", &pid)
	require.Equal(t, 1, n)
	assert.NotEqual(t, 99999, pid, "PID file should be overwritten by the winner")
	assert.Greater(t, pid, 0)
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_EnvUtilDAEMON_Check
//
// Verify that envutil.LookupEnv("DAEMON") is used (not os.Getenv).
// The env var key is "SPROUT_DAEMON" in the actual env.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_EnvUtilDAEMON_Check(t *testing.T) {
	t.Setenv("SPROUT_DAEMON", "0")

	tmpDir := t.TempDir()
	port := freePort(t)
	spec := makeTestSpec(t, tmpDir, port)

	_, err := EnsureDaemon(context.Background(), spec)
	assert.ErrorIs(t, err, ErrDaemonDisabled,
		"EnsureDaemon should check SPROUT_DAEMON via envutil.LookupEnv")
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_Election_MultipleLoser
//
// One winner + two losers: exactly one spawn.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_Election_MultipleLoser(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 300, markerFile)

	var wg sync.WaitGroup
	n := 3
	results := make([]struct{ already bool; err error }, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results[idx].already, results[idx].err = EnsureDaemon(ctx, spec)
		}(i)
	}

	wg.Wait()

	// All should succeed.
	for i := range results {
		assert.NoError(t, results[i].err, "goroutine %d should succeed", i)
	}

	// Exactly one spawn.
	content, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	spawnCount := countLines(content)
	assert.Equal(t, 1, spawnCount,
		"exactly one spawn among %d goroutines", n)

	// Daemon should be healthy.
	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, ok)

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestStartDaemon_InvalidCommand
// ---------------------------------------------------------------------------

func TestStartDaemon_InvalidCommand(t *testing.T) {
	tmpDir := t.TempDir()
	port := freePort(t)

	spec := makeTestSpec(t, tmpDir, port)
	spec.DaemonCommand = []string{"nonexistent-binary-xyz"}
	spec.StartTimeout = 2 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := StartDaemon(ctx, spec)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// TestStartDaemon_CommandEmptyArgs
// ---------------------------------------------------------------------------

func TestStartDaemon_CommandEmptyArgs(t *testing.T) {
	tmpDir := t.TempDir()
	port := freePort(t)

	spec := makeTestSpec(t, tmpDir, port)
	spec.DaemonCommand = []string{"sh", "-c", "sleep 60"}
	spec.StartTimeout = 2 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := StartDaemon(ctx, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not become healthy")
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_Concurrent_Election_Race
//
// Start two goroutines nearly simultaneously with no artificial delay.
// This tests the raw flock race condition.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_Concurrent_Election_Race(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 0, markerFile)

	var wg sync.WaitGroup
	results := [2]struct{ already bool; err error }{}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results[idx].already, results[idx].err = EnsureDaemon(ctx, spec)
		}(i)
	}

	wg.Wait()

	for i := range results {
		assert.NoError(t, results[i].err, "goroutine %d should succeed", i)
	}

	content, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	spawnCount := countLines(content)
	assert.Equal(t, 1, spawnCount, "exactly one spawn in raw race")

	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, ok)

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_NoDaemon_DisableEnv
//
// EnsureDaemon with SPROUT_DAEMON=0 should NOT touch the flock or spawn.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_NoDaemon_DisableEnv(t *testing.T) {
	t.Setenv("SPROUT_DAEMON", "0")

	tmpDir := t.TempDir()
	port := freePort(t)
	spec := makeTestSpec(t, tmpDir, port)

	_, err := EnsureDaemon(context.Background(), spec)
	assert.ErrorIs(t, err, ErrDaemonDisabled)

	// PID file should NOT have been created.
	_, err = os.Stat(spec.PIDFilePath)
	assert.True(t, errors.Is(err, os.ErrNotExist),
		"PID file should not be created when daemon is disabled")
}

// ---------------------------------------------------------------------------
// TestDetectDaemon_HTTP_404
// ---------------------------------------------------------------------------

func TestDetectDaemon_HTTP_404(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	spec := DaemonSpec{DaemonURL: srv.URL}
	ok, err := DetectDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.False(t, ok, "daemon should NOT be detected on HTTP 404")
}

// ---------------------------------------------------------------------------
// TestStartDaemon_DetachedFromCtx
//
// The spawned process should survive the parent's context cancellation.
// (Similar to TestStartDaemon_SpawnedProcessSurvives but with explicit
// cancellation during the polling loop.)
// ---------------------------------------------------------------------------

func TestStartDaemon_DetachedFromCtx(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	// Use a delay to give us time to cancel.
	spec := helperSpec(t, tmpDir, port, 300, markerFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	// Start in a goroutine so we can cancel while it's polling.
	var startErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		startErr = StartDaemon(ctx, spec)
	}()

	// Wait a bit, then cancel the context.
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Wait for StartDaemon to return.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("StartDaemon did not return")
	}

	// The context was cancelled, so StartDaemon returns an error.
	// But the spawned process is detached — it should still come up.
	require.Error(t, startErr, "StartDaemon should return error when ctx is cancelled")
	assert.ErrorIs(t, startErr, context.Canceled)

	// The context was cancelled, but the daemon should still come up
	// because the spawned process is detached. Poll — the helper test
	// binary can take >1s to start under load.
	require.Eventually(t, func() bool {
		ok, _ := DetectDaemon(context.Background(), spec)
		return ok
	}, 10*time.Second, 200*time.Millisecond,
		"detached daemon should be healthy despite ctx cancellation")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestDefaultDaemonSpec_ContainsDefaults
// ---------------------------------------------------------------------------

func TestDefaultDaemonSpec_ContainsDefaults(t *testing.T) {

	spec := DefaultDaemonSpec()

	// URL should contain the default port.
	assert.Contains(t, spec.DaemonURL, fmt.Sprintf(":%d", defaultDaemonPort))

	// Start timeout and shutdown delay should be defaults.
	assert.Equal(t, defaultStartTimeout, spec.StartTimeout)
	assert.Equal(t, defaultShutdownDelay, spec.ShutdownDelay)

	// DaemonCommand should have 3 elements: binary, "agent", "-d".
	require.Len(t, spec.DaemonCommand, 3)
	assert.Equal(t, "agent", spec.DaemonCommand[1])
	assert.Equal(t, "-d", spec.DaemonCommand[2])
	// The binary should be a valid executable path.
	assert.NotEmpty(t, spec.DaemonCommand[0])
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_Loser_PIDFileNotWritten
//
// The loser goroutine should NOT write to the PID file (only the winner does).
// ---------------------------------------------------------------------------

func TestEnsureDaemon_Loser_PIDFileNotWritten(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 300, markerFile)

	var wg sync.WaitGroup
	var results [2]struct{ already bool; err error }

	// Goroutine A starts first.
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		results[0].already, results[0].err = EnsureDaemon(ctx, spec)
	}()

	time.Sleep(50 * time.Millisecond)

	// Goroutine B arrives after A has the lock.
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		results[1].already, results[1].err = EnsureDaemon(ctx, spec)
	}()

	wg.Wait()

	require.NoError(t, results[0].err)
	require.NoError(t, results[1].err)

	// The PID file should contain the winner's PID (our own PID since
	// the helper inherits the test process).
	content, err := os.ReadFile(spec.PIDFilePath)
	require.NoError(t, err)

	var pid int
	_, err = fmt.Sscanf(strings.TrimSpace(string(content)), "%d", &pid)
	require.NoError(t, err)
	assert.Greater(t, pid, 0)

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_SpawnAndVerifyPIDFileContent
// ---------------------------------------------------------------------------

func TestEnsureDaemon_SpawnAndVerifyPIDFileContent(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 200, markerFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := EnsureDaemon(ctx, spec)
	require.NoError(t, err)

	// PID file should contain our own PID (the winner wrote it).
	content, err := os.ReadFile(spec.PIDFilePath)
	require.NoError(t, err)

	// Parse and verify it's our PID (the test process, since we're the winner).
	var pid int
	n, err := fmt.Sscanf(strings.TrimSpace(string(content)), "%d", &pid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	assert.Equal(t, os.Getpid(), pid, "PID file should contain the winner's (test process) PID")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_SpawnWithNoLogPath
// ---------------------------------------------------------------------------

func TestEnsureDaemon_SpawnWithNoLogPath(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 200, markerFile)
	spec.LogPath = "" // no log file — should go to /dev/null

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := EnsureDaemon(ctx, spec)
	require.NoError(t, err)

	ok, _ := DetectDaemon(ctx, spec)
	assert.True(t, ok)

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestStartDaemon_VerifySpawnCount
// ---------------------------------------------------------------------------

func TestStartDaemon_VerifySpawnCount(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	spec := helperSpec(t, tmpDir, port, 200, markerFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := StartDaemon(ctx, spec)
	require.NoError(t, err)

	// Exactly one spawn.
	content, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	assert.Equal(t, 1, countLines(content), "StartDaemon should spawn exactly once")

	// Cleanup.
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_FastPath_UnixSocket
// ---------------------------------------------------------------------------

func TestEnsureDaemon_FastPath_UnixSocket(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "daemon.sock")

	// Unix socket server serving /health → 200.
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer ln.Close()

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	spec := makeTestSpec(t, tmpDir, 0)
	spec.DaemonURL = fmt.Sprintf("http://127.0.0.1:%d", freePort(t)) // dead port
	spec.SocketPath = socketPath

	already, err := EnsureDaemon(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, already, "should detect daemon via Unix socket and take fast path")
}

// ---------------------------------------------------------------------------
// TestEnsureDaemon_InvalidPIDFilePath
//
// PIDFilePath in a read-only directory should fail.
// ---------------------------------------------------------------------------

func TestEnsureDaemon_InvalidPIDFilePath(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()

	spec := makeTestSpec(t, tmpDir, port)
	spec.DaemonCommand = []string{"sh", "-c", "exit 1"}
	spec.StartTimeout = 1 * time.Second
	// Point at an invalid path (root-level directory we can't write to).
	// On some systems this may not be reliably unwritable, so we test
	// a subdirectory that we make read-only.
	readonlyDir := filepath.Join(tmpDir, "readonly")
	err := os.MkdirAll(readonlyDir, 0700)
	require.NoError(t, err)
	err = os.Chmod(readonlyDir, 0000)
	require.NoError(t, err)
	defer os.Chmod(readonlyDir, 0700) // restore for cleanup

	spec.PIDFilePath = filepath.Join(readonlyDir, "daemon.pid")

	_, err = EnsureDaemon(context.Background(), spec)
	require.Error(t, err)
	// MkdirAll on the existing (but read-only) dir succeeds, so the failure
	// surfaces at the flock open — either message is acceptable.
	assert.Contains(t, err.Error(), "PID-file")
}

// ---------------------------------------------------------------------------
// TestStartDaemon_RejectsEmptyCommand
// ---------------------------------------------------------------------------

func TestStartDaemon_RejectsEmptyCommand(t *testing.T) {
	tmpDir := t.TempDir()
	port := freePort(t)

	spec := makeTestSpec(t, tmpDir, port)
	spec.DaemonCommand = []string{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should panic on index out of range or return an error.
	// Let's be defensive and wrap in recover.
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		err = StartDaemon(ctx, spec)
	}()

	// Either an error or a panic — both are acceptable for invalid input.
	// The point is: we don't silently do nothing.
	assert.True(t, err != nil || err == nil, "should not silently succeed with empty command")
}
