//go:build !js

// Package webui: remote SSH backend start/stop (split from ssh_launch.go).

package webui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func startRemoteSSHBackend(ctx context.Context, hostAlias, sessionKey, launcherURL, remoteWorkspacePath, remoteBinary string, forceRestart bool, logger *sshLaunchLogger) (remotePort int, remotePID int, reused bool, err error) {
	workspaceRaw := strings.TrimSpace(remoteWorkspacePath)
	if workspaceRaw == "" {
		workspaceRaw = "$HOME"
	}

	// SSH remote daemons use a fixed port so that multiple SSH sessions
	// to the same host can detect and reuse an existing daemon instead of
	// launching duplicates.

	script := strings.Join([]string{
		"set -e",
		"",
		// Source the user's shell startup files so that API-key environment
		// variables (typically exported in ~/.zshrc, ~/.bashrc, etc.) are
		// available to the daemon.  SSH non-interactive sessions skip these
		// files, but daemon startup depends on the keys they define.
		//
		// Most rc files short-circuit under non-interactive shells via a
		// guard like `[[ $- != *i* ]] && return` or `case $- in *i*) ;;
		// *) return;; esac`.  An earlier revision tried to bypass that
		// guard with `set -i`, but `i` is not a valid bash option (it's
		// a zsh-ism) — bash returns exit 2, which under `set -e` aborts
		// the script before the daemon is ever launched.
		//
		// Instead, we spawn an interactive subshell that sources the rc
		// file as if it were the user's login rc, then export any env
		// variables it set.  `bash --rcfile FILE -ic 'env -0'` (and the
		// zsh/sh equivalents) guarantees `$-` includes `i` so the rc
		// file's non-interactive guard passes, and NUL-delimited output
		// lets us re-import the environment safely even if a value
		// contains a newline.
		`_src_rc() {`,
		`  _file="$1"; _kind="$2"`,
		`  [ -f "$_file" ] || return 0`,
		`  set +e`,
		`  case "$_kind" in`,
		`    zsh) _env=$(zsh -ic '. "$1" >/dev/null 2>&1 && env -0' -- "$_file" 2>/dev/null) ;;`,
		`    bash) _env=$(bash --rcfile "$_file" -ic 'env -0' 2>/dev/null) ;;`,
		`    sh) _env=$(sh -ic '. "$1" >/dev/null 2>&1 && env -0' -- "$_file" 2>/dev/null) ;;`,
		`    *) return 0 ;;`,
		`  esac`,
		`  if [ -n "$_env" ]; then`,
		`    while IFS= read -r -d '' _line; do`,
		`      case "$_line" in`,
		`        *=*) export "$_line" ;;`,
		`      esac`,
		`    done <<< "$_env"`,
		`  fi`,
		`  unset _file _kind _env _line`,
		`  set -e`,
		`}`,
		`case "$(basename "${SHELL:-sh}")" in`,
		`  zsh) _src_rc "$HOME/.zshenv" zsh; _src_rc "$HOME/.zprofile" zsh; _src_rc "$HOME/.zshrc" zsh ;;`,
		`  bash) _src_rc "$HOME/.bash_profile" bash; _src_rc "$HOME/.bashrc" bash ;;`,
		`  fish) ;;`,
		`  *)   _src_rc "$HOME/.profile" sh ;;`,
		`esac`,
		`unset -f _src_rc`,
		`DAEMON_PORT=` + fmt.Sprintf("%d", DaemonPort),
		`FORCE_RESTART=` + map[bool]string{true: "1", false: "0"}[forceRestart],
		"",
		"# check_existing_daemon: health-probe port DAEMON_PORT and return PID if healthy.",
		`check_existing_daemon() {`,
		`  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then`,
		`    return 1`,
		`  fi`,
		`  local resp=""`,
		`  if command -v curl >/dev/null 2>&1; then`,
		`    resp=$(curl -sf -m 3 "http://127.0.0.1:$DAEMON_PORT/health" 2>/dev/null) || return 1`,
		`  else`,
		`    resp=$(wget -qO- -T 3 "http://127.0.0.1:$DAEMON_PORT/health" 2>/dev/null) || return 1`,
		`  fi`,
		`  # Look for a JSON response with a "port" field to confirm sprout.`,
		`  case "$resp" in`,
		`    *'"status":"ok"'*) ;;`,
		`    *) return 1 ;;`,
		`  esac`,
		`  # Find the PID listening on the daemon port.`,
		`  local pid=""`,
		`  if command -v lsof >/dev/null 2>&1; then`,
		`    pid=$(lsof -ti tcp:$DAEMON_PORT -sTCP:LISTEN 2>/dev/null | head -1)`,
		`  elif command -v ss >/dev/null 2>&1; then`,
		`    pid=$(ss -tlnpH "sport = :$DAEMON_PORT" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1)`,
		`  elif command -v netstat >/dev/null 2>&1; then`,
		`    pid=$(netstat -tlnp 2>/dev/null | grep ":$DAEMON_PORT " | sed -n 's/.*LISTEN[[:space:]]*\([0-9]*\)\/.*/\1/p' | head -1)`,
		`  elif command -v fuser >/dev/null 2>&1; then`,
		`    pid=$(fuser "$DAEMON_PORT/tcp" 2>/dev/null | head -1)`,
		`  fi`,
		`  [ -n "$pid" ] && [ "$pid" -gt 0 ] 2>/dev/null && echo "$pid"`,
		`  return 0`,
		`}`,
		"",
		"# Acquire a cross-session lock so concurrent SSH launches to this host",
		"# don't race to kill/start the daemon. Uses flock on Linux; falls back",
		"# to a mkdir-based mutex on systems without flock (macOS).",
		`LOCK_DIR="$HOME/.cache/sprout-webui"`,
		`LOCK_FILE="$LOCK_DIR/daemon-start.lock"`,
		`LOCK_HELD=""`,
		`mkdir -p "$LOCK_DIR" 2>/dev/null || true`,
		`if command -v flock >/dev/null 2>&1; then`,
		`    exec 9>"$LOCK_FILE"`,
		`    if flock -w 30 9 2>/dev/null; then`,
		`        LOCK_HELD=1`,
		`    fi`,
		`else`,
		`    for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do`,
		`        if mkdir "$LOCK_FILE" 2>/dev/null; then`,
		`            LOCK_HELD=1`,
		`            break`,
		`        fi`,
		`        sleep 2`,
		`    done`,
		`fi`,
		`# Ensure the lock is released on ANY exit path (set -e failure, premature`,
		`# daemon death, explicit exit). flock auto-releases on fd close, but the`,
		`# mkdir fallback leaves a stale lock directory that blocks subsequent`,
		`# launches for the full 30-second timeout window.`,
		`trap 'if [ -n "$LOCK_HELD" ]; then if command -v flock >/dev/null 2>&1; then flock -u 9 2>/dev/null || true; else rmdir "$LOCK_FILE" 2>/dev/null || true; fi; fi' EXIT`,
		"",
		"# If force-restart was requested, kill the old daemon now (under the lock).",
		`if [ "$FORCE_RESTART" = "1" ]; then`,
		`    if [ "$LOCK_HELD" = "1" ]; then`,
		`        if command -v lsof >/dev/null 2>&1; then`,
		`            pid=$(lsof -ti tcp:"$DAEMON_PORT" -sTCP:LISTEN 2>/dev/null | head -1)`,
		`        elif command -v ss >/dev/null 2>&1; then`,
		`            pid=$(ss -tlnpH "sport = :$DAEMON_PORT" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1)`,
		`        else`,
		`            pid=""`,
		`        fi`,
		`        [ -n "$pid" ] && [ "$pid" -gt 0 ] 2>/dev/null && kill "$pid" 2>/dev/null || true`,
		`        # Wait for the daemon's health endpoint to go down — SIGTERM`,
		`        # allows graceful shutdown, so the dying daemon can still`,
		`        # respond to check_existing_daemon and be incorrectly reused.`,
		`        for _ in 1 2 3 4 5; do`,
		`            if command -v curl >/dev/null 2>&1; then`,
		`                curl -sf -m 1 "http://127.0.0.1:$DAEMON_PORT/health" >/dev/null 2>&1 || break`,
		`            elif command -v wget >/dev/null 2>&1; then`,
		`                wget -qO- -T 1 "http://127.0.0.1:$DAEMON_PORT/health" >/dev/null 2>&1 || break`,
		`            else`,
		`                break`,
		`            fi`,
		`            sleep 1`,
		`        done`,
		`    fi`,
		`fi`,
		"",
		"# Try to reuse an existing daemon on this host.",
		`EXISTING_PID=$(check_existing_daemon || true)`,
		`if [ -n "$EXISTING_PID" ]; then`,
		`  printf "%s\\n%s\\n%s\\nreused\\n" "SPROUT_DAEMON_RESULT_START" "$DAEMON_PORT" "$EXISTING_PID"`,
		`  exit 0`,
		`fi`,
		"",
		"# No existing daemon — start one.",
		`mkdir -p "$HOME/.cache/sprout-webui/logs"`,
		fmt.Sprintf("WORKSPACE_RAW=%s", shellEscapeSSH(workspaceRaw)),
		`WORKSPACE_PATH="$WORKSPACE_RAW"`,
		`case "$WORKSPACE_PATH" in`,
		`  '$HOME'|'${HOME}') WORKSPACE_PATH="$HOME" ;;`,
		`  '$HOME/'*) WORKSPACE_PATH="$HOME/${WORKSPACE_PATH#\$HOME/}" ;;`,
		`  '${HOME}/'*) WORKSPACE_PATH="$HOME/${WORKSPACE_PATH#\${HOME}/}" ;;`,
		`  '~') WORKSPACE_PATH="$HOME" ;;`,
		`  '~/'*) WORKSPACE_PATH="$HOME/${WORKSPACE_PATH#~/}" ;;`,
		`esac`,
		// Validate the workspace path is accessible without cd-ing into it.
		// The daemon runs host-level from $HOME to support multi-workspace.
		`test -d "$WORKSPACE_PATH" || { echo "remote workspace path is not accessible: $WORKSPACE_RAW" >&2; exit 1; }`,
		fmt.Sprintf(`LOG_FILE="$HOME/.cache/sprout-webui/logs/%s.log"`, sanitizeRemoteLogName(hostAlias)),
		// Launch the daemon from $HOME (not the workspace directory) using
		// the user's main config.  No --isolated-config — this is a host-level
		// daemon that serves multiple workspaces via per-client context.
		// Explicitly cd to $HOME to ensure daemonRoot is the user's home,
		// regardless of what the SSH login shell's profile does.
		fmt.Sprintf(
			`cd "$HOME" 2>/dev/null || cd /tmp; `+
				`nohup env BROWSER=none SPROUT_SSH_HOST_ALIAS=%s SPROUT_SSH_SESSION_KEY=%s SPROUT_SSH_LAUNCHER_URL=%s SPROUT_SSH_HOME="%s" SPROUT_SKIP_CONNECTION_CHECK=1 %s agent --daemon --web-port "$DAEMON_PORT" >"$LOG_FILE" 2>&1 < /dev/null &`,
			shellEscapeSSH(hostAlias),
			shellEscapeSSH(sessionKey),
			shellEscapeSSH(launcherURL),
			shellEscapeSSH(remoteWorkspacePath),
			shellEscapeSSH(remoteBinary),
		),
		"REMOTE_PID=$!",
		`# Poll the daemon's health endpoint until it responds or the process dies.`,
		`if command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1; then`,
		`  START_WAIT=0`,
		`  MAX_WAIT=15`,
		`  while [ $START_WAIT -lt $MAX_WAIT ]; do`,
		`    if ! kill -0 "$REMOTE_PID" 2>/dev/null; then`,
		`      echo "ERROR: sprout daemon exited prematurely on port $DAEMON_PORT" >&2`,
		`      echo "Last 50 lines from daemon log:" >&2`,
		`      tail -n 50 "$LOG_FILE" 2>&1 | while IFS= read -r line; do echo "  $line" >&2; done`,
		`      exit 1`,
		`    fi`,
		`    HEALTH=""`,
		`    if command -v curl >/dev/null 2>&1; then`,
		`      HEALTH=$(curl -sf -m 1 "http://127.0.0.1:$DAEMON_PORT/health" 2>/dev/null) || true`,
		`    else`,
		`      HEALTH=$(wget -qO- -T 1 "http://127.0.0.1:$DAEMON_PORT/health" 2>/dev/null) || true`,
		`    fi`,
		`    case "$HEALTH" in`,
		`      *'"status":"ok"'*) break ;;`,
		`    esac`,
		`    sleep 1`,
		`    START_WAIT=$((START_WAIT + 1))`,
		`  done`,
		`else`,
		`  sleep 1`,
		`  if ! kill -0 "$REMOTE_PID" 2>/dev/null; then`,
		`    echo "ERROR: sprout daemon failed to start on port $DAEMON_PORT — another daemon may already be running on this host" >&2`,
		`    exit 1`,
		`  fi`,
		`fi`,
		`printf "%s\n%s\n%s\nnew\n" "SPROUT_DAEMON_RESULT_START" "$DAEMON_PORT" "$REMOTE_PID"`,
	}, "\n")

	cmd := newSSHCommandContext(ctx, hostAlias, script)
	out, err := runSSHLoggedCommand(logger, "start-remote-backend", fmt.Sprintf("ssh %s start remote backend", hostAlias), cmd)
	if err != nil {
		return 0, 0, false, fmt.Errorf("start remote backend for %s: %w", hostAlias, err)
	}

	// Extract the result block between sentinel markers. RC file sourcing
	// can emit stray stdout (fortune, neofetch, MOTD) that would otherwise
	// corrupt the line-based parsing.
	result, err := extractSentinelResult(string(out), "SPROUT_DAEMON_RESULT_START")
	if err != nil {
		return 0, 0, false, fmt.Errorf("failed to determine remote backend port for %s: %w", hostAlias, err)
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) < 3 { // port + pid + status
		return 0, 0, false, fmt.Errorf("unexpected remote backend output format for %s", hostAlias)
	}
	remotePort, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || remotePort <= 0 {
		return 0, 0, false, fmt.Errorf("invalid remote web port for %s", hostAlias)
	}
	remotePID, _ = strconv.Atoi(strings.TrimSpace(lines[1]))
	wasReused := strings.TrimSpace(lines[2]) == "reused"
	return remotePort, remotePID, wasReused, nil
}
