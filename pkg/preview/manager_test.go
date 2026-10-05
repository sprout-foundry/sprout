package preview

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeManifestRaw writes a raw starter manifest JSON document into
// .sprout/starter.json under root.
func writeManifestRaw(t *testing.T, root, manifest string) {
	t.Helper()
	dir := filepath.Join(root, ".sprout")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "starter.json"), []byte(manifest), 0o644))
}

// writeManifest writes a starter manifest declaring dev command and port
// (each omitted when empty/zero) — the .sprout/starter.json the manager
// reads.
func writeManifest(t *testing.T, root, dev string, port int) {
	t.Helper()
	var manifest string
	switch {
	case dev == "" && port == 0:
		manifest = `{"starter":{"id":"fixture","version":"0.0.1"}}`
	case dev == "":
		manifest = fmt.Sprintf(`{"starter":{"id":"fixture","version":"0.0.1"},"dev_port":%d}`, port)
	default:
		manifest = fmt.Sprintf(`{"starter":{"id":"fixture","version":"0.0.1"},"dev":%q,"dev_port":%d}`, dev, port)
	}
	writeManifestRaw(t, root, manifest)
}

// freePort returns a currently-free port on 127.0.0.1 (verify's pattern).
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startLocalServer serves HTTP on the given port (a real local listener, no
// shell involved) and closes it when the test ends. It stands in for an
// already-running dev server (the detect path).
func startLocalServer(t *testing.T, port int) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("preview app"))
	}))
	s.Listener = ln
	s.Start()
	t.Cleanup(s.Close)
	return s
}

// fixtureServerJS is the fixture dev app (verify's fixture pattern): a
// dependency-free Node server that binds the port and records its pid so a
// test can prove a restart replaced the process.
const fixtureServerJS = `const http = require('http');
const fs = require('fs');
const port = parseInt(process.argv[2], 10);
const server = http.createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/plain' });
  res.end('fixture app');
});
server.listen(port, '127.0.0.1', () => {
  fs.writeFileSync('preview.pid', String(process.pid));
});
`

// fixtureIdleJS is a dev command that stays alive but never binds the port
// (the ready-timeout path): it records its pid (so a test can prove a
// single spawn) and exits on its own well after the test ends.
const fixtureIdleJS = `const fs = require('fs');
const port = parseInt(process.argv[2], 10);
fs.appendFileSync('idle.pids', process.pid + '\\n');
setTimeout(() => process.exit(0), 30000);
`

// writeFixtureApp writes the fixture dev app plus a starter manifest
// declaring it as the dev command for port.
func writeFixtureApp(t *testing.T, root string, port int) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "server.js"), []byte(fixtureServerJS), 0o644))
	writeManifest(t, root, "node server.js "+strconv.Itoa(port), port)
}

// nodeAvailable skips the real-spawn tests when node is not on PATH
// (verify's nodeAvailable pattern).
func nodeAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available on this platform; skipping fixture-server tests")
	}
}

// shAvailable skips the tests whose dev command is a shell builtin
// (verify's shAvailable pattern).
func shAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available on this platform")
	}
}

// readPIDFile reads the fixture server's pid file, retrying until the
// (asynchronous) write has landed.
func readPIDFile(t *testing.T, root string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(root, "preview.pid"))
		if err == nil {
			if pid, perr := strconv.Atoi(string(bytes.TrimSpace(data))); perr == nil {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid file not written under %s within 3s", root)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// killPID kills a process by pid (os.Process.Kill: SIGKILL / TerminateProcess).
func killPID(t *testing.T, pid int) {
	t.Helper()
	proc, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.NoError(t, proc.Kill())
}

// assertPortUp asserts something answers an HTTP GET on port.
func assertPortUp(t *testing.T, port int) {
	t.Helper()
	require.True(t, probeHTTP(port), "expected the dev port %d to answer", port)
}

// assertPortClosed asserts nothing answers on port (retrying briefly: a
// killed process's socket can take a moment to release).
func assertPortClosed(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if !probeHTTP(port) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("dev port %d still answering after 3s", port)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestStateNoManifest pins the "no starter manifest" case: the manager never
// guesses a dev command, so the state is stopped with a reason — both from
// a plain read and from an explicit Start.
func TestStateNoManifest(t *testing.T) {
	root := t.TempDir()
	m := New(root)

	st := m.State()
	assert.Equal(t, StatusStopped, st.Status)
	assert.Empty(t, st.URL)
	assert.False(t, st.Detected)
	assert.Contains(t, st.Error, "no starter manifest")

	st = m.Start(context.Background())
	assert.Equal(t, StatusStopped, st.Status)
	assert.Contains(t, st.Error, "no starter manifest")
}

// TestStartWithoutDevDeclaration pins the manifest cases that declare no
// usable dev server: no dev command, or a dev command without a port.
func TestStartWithoutDevDeclaration(t *testing.T) {
	t.Run("manifest without dev command", func(t *testing.T) {
		root := t.TempDir()
		writeManifest(t, root, "", 0)
		m := New(root)

		st := m.Start(context.Background())
		assert.Equal(t, StatusStopped, st.Status)
		assert.Contains(t, st.Error, "no dev command")
	})

	t.Run("dev command without port", func(t *testing.T) {
		root := t.TempDir()
		writeManifest(t, root, "npm run dev", 0)
		m := New(root)

		st := m.Start(context.Background())
		assert.Equal(t, StatusStopped, st.Status)
		assert.Contains(t, st.Error, "no dev port")
	})
}

// TestStartInvalidManifest pins that a corrupt manifest is a hard error,
// never a guess: the state settles stopped with the invalid-manifest reason.
func TestStartInvalidManifest(t *testing.T) {
	root := t.TempDir()
	writeManifestRaw(t, root, `{invalid json`)
	m := New(root)

	st := m.Start(context.Background())
	assert.Equal(t, StatusStopped, st.Status)
	assert.Contains(t, st.Error, "invalid")
}

// TestStartDetectsRunningServer pins the detect path: a server already
// answering on the manifest's dev port is adopted (running, detected), and
// a second Start adopts the same server rather than starting a second one.
func TestStartDetectsRunningServer(t *testing.T) {
	root := t.TempDir()
	port := freePort(t)
	startLocalServer(t, port)
	writeManifest(t, root, "npm run dev", port)

	m := New(root)
	st := m.Start(context.Background())
	assert.Equal(t, StatusRunning, st.Status)
	assert.Equal(t, devURL(port), st.URL)
	assert.True(t, st.Detected)
	assert.Empty(t, st.Error)

	st = m.Start(context.Background())
	assert.Equal(t, StatusRunning, st.Status)
	assert.True(t, st.Detected, "a second Start must adopt the running server, not start another")
}

// TestStartFailedCommand pins the failed path: a dev command that exits
// before the port answers settles failed, with its last output in the
// reason.
func TestStartFailedCommand(t *testing.T) {
	shAvailable(t)
	root := t.TempDir()
	port := freePort(t)
	writeManifest(t, root, "echo boom 1>&2; exit 1", port)

	m := New(root)
	st := m.Start(context.Background())
	assert.Equal(t, StatusFailed, st.Status)
	assert.Empty(t, st.URL)
	assert.Contains(t, st.Error, "exited before the dev port")
	assert.Contains(t, st.Error, "boom")
}

// TestStartTimeout pins the ready-timeout path: a dev command that stays
// alive but never binds the port settles failed when the ready timeout
// elapses (the process group is killed with it).
func TestStartTimeout(t *testing.T) {
	nodeAvailable(t)
	root := t.TempDir()
	port := freePort(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "server-idle.js"), []byte(fixtureIdleJS), 0o644))
	writeManifest(t, root, "node server-idle.js "+strconv.Itoa(port), port)

	m := New(root, WithReadyTimeout(time.Second))
	st := m.Start(context.Background())
	assert.Equal(t, StatusFailed, st.Status)
	assert.Contains(t, st.Error, "did not become ready within")
	assert.Contains(t, st.Error, fmt.Sprintf("dev port %d", port))

	// Stop on a failed (already-dead) start is a clean no-op.
	st = m.Stop()
	assert.Equal(t, StatusStopped, st.Status)
}

// TestStartCancelledContext pins the cancellation path: a start whose
// context is cancelled settles stopped (the spawned process group is
// killed), not failed.
func TestStartCancelledContext(t *testing.T) {
	nodeAvailable(t)
	root := t.TempDir()
	port := freePort(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "server-idle.js"), []byte(fixtureIdleJS), 0o644))
	writeManifest(t, root, "node server-idle.js "+strconv.Itoa(port), port)

	m := New(root, WithReadyTimeout(10*time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	st := m.Start(ctx)
	assert.Equal(t, StatusStopped, st.Status, "a cancelled start settles stopped, not failed")
	assert.Contains(t, st.Error, "cancelled")
}

// TestStopOwnedServer pins the owned stop: the dev server is killed and
// the port released; Stop is idempotent.
func TestStopOwnedServer(t *testing.T) {
	nodeAvailable(t)
	root := t.TempDir()
	port := freePort(t)
	writeFixtureApp(t, root, port)

	m := New(root)
	t.Cleanup(func() { m.Stop() })

	st := m.Start(context.Background())
	assert.Equal(t, StatusRunning, st.Status)
	assert.Equal(t, devURL(port), st.URL)
	assert.False(t, st.Detected)
	assertPortUp(t, port)

	st = m.Stop()
	assert.Equal(t, StatusStopped, st.Status)
	assert.Empty(t, st.URL)
	assertPortClosed(t, port)

	st = m.Stop()
	assert.Equal(t, StatusStopped, st.Status, "a second stop is a no-op")
}

// TestRestartOwnedServer pins restart: the dev server process is replaced
// (a new pid) and the port serves again.
func TestRestartOwnedServer(t *testing.T) {
	nodeAvailable(t)
	root := t.TempDir()
	port := freePort(t)
	writeFixtureApp(t, root, port)

	m := New(root)
	t.Cleanup(func() { m.Stop() })

	st := m.Start(context.Background())
	require.Equal(t, StatusRunning, st.Status)
	pid1 := readPIDFile(t, root)

	st = m.Restart(context.Background())
	assert.Equal(t, StatusRunning, st.Status)
	assert.False(t, st.Detected)
	pid2 := readPIDFile(t, root)
	assert.NotEqual(t, pid1, pid2, "restart must replace the dev server process")
	assertPortUp(t, port)
}

// TestRestartDetectedServerIsRedetect pins that a restart of a detected
// (external) server re-detects it — the manager cannot restart what it does
// not own — and that Stop leaves it alone.
func TestRestartDetectedServerIsRedetect(t *testing.T) {
	root := t.TempDir()
	port := freePort(t)
	startLocalServer(t, port)
	writeManifest(t, root, "npm run dev", port)

	m := New(root)
	st := m.Start(context.Background())
	require.Equal(t, StatusRunning, st.Status)
	require.True(t, st.Detected)

	st = m.Restart(context.Background())
	assert.Equal(t, StatusRunning, st.Status)
	assert.True(t, st.Detected, "an external server cannot be restarted: it is re-detected")

	st = m.Stop()
	assert.Equal(t, StatusRunning, st.Status, "Stop cannot stop an external server")
	assert.True(t, st.Detected)
}

// TestDetectedServerDeathSettlesStopped pins the detected-server liveness
// contract: when the external server dies, the next State read settles the
// state to stopped (not failed — the manager did not start it).
func TestDetectedServerDeathSettlesStopped(t *testing.T) {
	root := t.TempDir()
	port := freePort(t)
	s := startLocalServer(t, port)
	writeManifest(t, root, "npm run dev", port)

	m := New(root)
	st := m.Start(context.Background())
	require.Equal(t, StatusRunning, st.Status)

	s.Close()

	st = m.State()
	assert.Equal(t, StatusStopped, st.Status)
	assert.Contains(t, st.Error, "no longer responding")
}

// TestCrashedServerSettlesFailedThenRestartRecovers pins the liveness
// monitor: a dev server that dies while running settles the state to
// failed (within the monitor's poll window), and a restart recovers.
func TestCrashedServerSettlesFailedThenRestartRecovers(t *testing.T) {
	nodeAvailable(t)
	root := t.TempDir()
	port := freePort(t)
	writeFixtureApp(t, root, port)

	m := New(root)
	t.Cleanup(func() { m.Stop() })

	st := m.Start(context.Background())
	require.Equal(t, StatusRunning, st.Status)

	pid := readPIDFile(t, root)
	killPID(t, pid)

	deadline := time.Now().Add(15 * time.Second)
	for {
		st = m.State()
		if st.Status == StatusFailed {
			break
		}
		require.True(t, time.Now().Before(deadline),
			"the liveness monitor must settle the crashed server to failed (got %s)", st.Status)
		time.Sleep(100 * time.Millisecond)
	}
	assert.Contains(t, st.Error, "exited")
	assertPortClosed(t, port)

	st = m.Restart(context.Background())
	assert.Equal(t, StatusRunning, st.Status)
	assertPortUp(t, port)
}

// TestStartWhileStartInFlight pins the concurrency guard: a Start that
// arrives while a first start is still settling waits for that cycle and
// reports its outcome — it does not spawn a second server.
func TestStartWhileStartInFlight(t *testing.T) {
	nodeAvailable(t)
	root := t.TempDir()
	port := freePort(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "server-idle.js"), []byte(fixtureIdleJS), 0o644))
	writeManifest(t, root, "node server-idle.js "+strconv.Itoa(port), port)

	m := New(root, WithReadyTimeout(time.Second))
	done := make(chan State, 1)
	go func() {
		done <- m.Start(context.Background())
	}()

	// Wait until the first start is in flight (starting).
	deadline := time.Now().Add(5 * time.Second)
	for m.State().Status != StatusStarting {
		require.True(t, time.Now().Before(deadline), "expected the first start to be in flight")
		time.Sleep(50 * time.Millisecond)
	}

	// A concurrent Start waits for the in-flight cycle and reports its
	// outcome (the ready timeout), without spawning a second server.
	st := m.Start(context.Background())
	assert.Equal(t, StatusFailed, st.Status)
	assert.Contains(t, st.Error, "did not become ready")

	st = <-done
	assert.Equal(t, StatusFailed, st.Status)

	data, err := os.ReadFile(filepath.Join(root, "idle.pids"))
	require.NoError(t, err)
	assert.Len(t, bytes.Split(bytes.TrimSpace(data), []byte("\n")), 1,
		"exactly one dev server process must have been spawned")
}
