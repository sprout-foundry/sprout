//go:build !js

package webui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const sleeperEnv = "SPROUT_WEBUI_TEST_SLEEPER"

// osAbs turns a slash-rooted fixture like "/ws/a.go" into a path that is
// absolute on the host OS: Windows needs a volume for filepath.IsAbs.
func osAbs(p string) string {
	if runtime.GOOS == "windows" {
		return `C:` + filepath.FromSlash(p)
	}
	return p
}

// jsonQuote encodes s as a JSON string literal, so Windows paths with
// backslashes can be spliced into hand-written request bodies.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// runSleeperIfRequested turns the test binary into an idle child process
// when started by startSleeper. Called first thing in TestMain.
func runSleeperIfRequested() {
	if os.Getenv(sleeperEnv) == "1" {
		time.Sleep(5 * time.Minute)
		os.Exit(0)
	}
}

// startSleeper spawns a live, killable child process for PID fixtures.
// It re-execs the test binary instead of `sleep` so it works where no
// POSIX coreutils are on PATH, and PID 1 is not a live process on Windows.
func startSleeper(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // G204: re-executes this test binary as a live sleeper
	cmd.Env = append(os.Environ(), sleeperEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// setTestHome points os.UserHomeDir at dir on every platform: Windows reads
// USERPROFILE, not HOME.
func setTestHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
