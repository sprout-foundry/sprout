//go:build !js

// Package webui: remote SSH backend process stop and verification (split from ssh_launch.go).

package webui

import (
	"fmt"
	"log/slog"
	"strings"
)

func stopRemoteSSHBackend(hostAlias string, remotePID int) error {
	if strings.TrimSpace(hostAlias) == "" || remotePID <= 0 {
		return nil
	}

	// Verify the remote PID still belongs to a sprout daemon before killing
	// it. Between sessions, the OS may have recycled the PID for an unrelated
	// process (database, user shell, system service). We kill only if the
	// process's command line contains "sprout" — otherwise we leave it alone
	// and log a warning. This is a heuristic, not a guarantee: a determined
	// attacker could name their process "sprout", but the blast radius of a
	// false positive (killing an innocent system process) is worse than the
	// risk of leaving a stale daemon running.
	if !verifyRemoteSproutPID(hostAlias, remotePID) {
		webuiLogger.Warn("refusing to stop remote process because it is not a sprout daemon", slog.Int("remote_pid", remotePID), slog.String("host_alias", hostAlias), slog.String("reason", "PID may have been recycled"))
		return nil
	}

	cmd := newSSHCommand(hostAlias, fmt.Sprintf("kill %d >/dev/null 2>&1 || true", remotePID))
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("failed to kill SSH process: %s: %w", msg, err)
	}
	return nil
}

// verifyRemoteSproutPID checks whether the remote process identified by remotePID
// is a sprout daemon by inspecting its command line. Returns true if the PID
// is alive and its command line contains "sprout" or "agent --daemon". Returns
// false if the PID is dead, the command line doesn't match, or the check fails
// (conservative: don't kill what we can't verify).
func verifyRemoteSproutPID(hostAlias string, remotePID int) bool {
	if remotePID <= 0 {
		return false
	}

	// Build a script that checks the process command line across different
	// platforms. Uses /proc/$PID/cmdline on Linux and ps on macOS/BSD.
	// Falls back to "alive but unknown" (returns "unknown") if neither works.
	script := strings.Join([]string{
		"set +e",
		fmt.Sprintf("PID=%d", remotePID),
		`if ! kill -0 "$PID" 2>/dev/null; then`,
		`  echo "dead"`,
		`  exit 0`,
		`fi`,
		// Linux: /proc/$PID/cmdline is null-separated
		`if [ -r "/proc/$PID/cmdline" ]; then`,
		fmt.Sprintf(`  CMDLINE=$(tr "\\0" " " < /proc/$PID/cmdline 2>/dev/null)`),
		`  case "$CMDLINE" in`,
		`    *"sprout agent --daemon"*|*"sprout serve"*|*"sprout --daemon"*) echo "sprout"; exit 0 ;;`,
		`    *) echo "other"; exit 0 ;;`,
		`  esac`,
		`fi`,
		// macOS / BSD: ps command
		`if command -v ps >/dev/null 2>&1; then`,
		`  CMDLINE=$(ps -o command= -p "$PID" 2>/dev/null | head -1)`,
		`  case "$CMDLINE" in`,
		`    *"sprout agent --daemon"*|*"sprout serve"*|*"sprout --daemon"*) echo "sprout"; exit 0 ;;`,
		`    *) echo "other"; exit 0 ;;`,
		`  esac`,
		`fi`, // Couldn't determine — conservative "don't kill"
		`echo "unknown"`,
	}, "\n")

	cmd := newSSHCommand(hostAlias, script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// SSH error — can't verify, don't kill
		return false
	}
	// Extract the last non-empty line — login-profile stdout (MOTD,
	// fortune) can prepend output that would otherwise corrupt the
	// exact-match comparison against "sprout".
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	result := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			result = line
			break
		}
	}
	return result == "sprout"
}
