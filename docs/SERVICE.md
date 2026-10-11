# Sprout Daemon Service Guide

The sprout daemon runs the web UI as a persistent background service, managed by the OS service manager (launchd on macOS, systemd on Linux). This document covers installation, lifecycle, configuration, troubleshooting, and the security model.

## Commands

All service management is done through `sprout service`:

```
sprout service install    # Install and start the daemon
sprout service start      # Start an installed daemon
sprout service stop       # Stop the running daemon
sprout service uninstall  # Stop and remove the daemon
sprout service status     # Show daemon status
sprout service diagnose   # Run diagnostic checks
```

The `install` and `uninstall` subcommands support `-y`/`--yes` to skip confirmation prompts. `install` also takes `--root <dir>` (see [Daemon root](#daemon-root)).

## Install

```bash
sprout service install
sprout service install --root ~/dev   # scope the daemon to ~/dev
```

This command:

1. Detects the OS (macOS → launchd, Linux → systemd)
2. Captures current API keys from the environment into `service.env` in the state directory
3. Generates a service configuration (launchd plist or systemd unit) that contains no secrets
4. Installs and starts the service

The state directory is `~/.local/state/sprout` by default (`$SPROUT_STATE_DIR` or `$XDG_STATE_HOME/sprout` when set).

### What gets created

| OS | Service file | Log files |
|---|---|---|
| macOS | `~/Library/LaunchAgents/com.sprout.daemon.plist` | `~/.local/state/sprout/logs/daemon.stdout.log`, `daemon.stderr.log` (rotated); `~/Library/Logs/sprout/daemon.log` (launchd startup/crash output) |
| Linux | `~/.config/systemd/user/sprout.service` | `~/.local/state/sprout/logs/daemon.stdout.log`, `daemon.stderr.log` (rotated) |

### Where secrets live

The install command captures every `*_API_KEY`, `*_TOKEN`, `*_SECRET`, `*_ACCESS_KEY` and `*_SECRET_KEY` variable (plus `SPROUT_PROVIDER*` / `SPROUT_SUBAGENT_*` settings) from the installing shell into `service.env` (mode `0600`). Secrets are never written into the service file:

- **Linux**: the systemd unit references the file with `EnvironmentFile=`.
- **macOS**: launchd has no equivalent, so the plist only sets `SPROUT_SERVICE`, `HOME`, `SPROUT_DAEMON_ROOT` and `PATH`, and the daemon loads `service.env` itself at startup. Variables already present in the daemon's environment take precedence over the file. Only a count of loaded variables is logged, never names or values.

Providers also resolve keys stored with `sprout keys set <provider>` (the credential store) when no environment variable is set, so keys kept there need not be in `service.env` at all.

Older installs inlined the captured secrets into the launchd plist in plaintext. Run `sprout service install` again to rewrite the plist clean (it lists the variable names it removed and warns about any that the current shell no longer provides). `sprout service start`/`restart` reuse the plist on disk, so they warn about an old plist but do not rewrite it, and `sprout service diagnose` flags it by variable name. Consider rotating any key that was in an old plist that was backed up or shared.

### Daemon root

`--root <dir>` sets `SPROUT_DAEMON_ROOT` in the service file. The directory must exist; the default is your home directory. The daemon root scopes daemon-level storage and the web UI's workspace browser: workspaces you pick in the web UI must be inside it. It does not choose the agent's workspace; the web UI asks for that explicitly. Without systemd (PID-file mode on Linux) `--root` is ignored; set `SPROUT_DAEMON_ROOT` when starting the daemon instead.

### Environment file

**File format** (line-delimited `KEY=VALUE`):

```
SPROUT_ANTHROPIC_API_KEY=sk-ant-...
SPROUT_OPENAI_API_KEY=sk-...
```

To update API keys after install, either:
- Re-run `sprout service install` from a shell that exports them
- Edit `service.env` directly, then restart (`sprout service restart`)
- Store them with `sprout keys set <provider>`
- Send SIGHUP to the running process for live config reload (see [Live Reload](#live-reload))

## Start / Stop

```bash
sprout service start
sprout service stop
```

- **Start**: Starts the daemon via the service manager. On macOS, uses `launchctl kickstart`. On Linux, uses `systemctl start`.
- **Stop**: Stops the daemon. On macOS, uses `launchctl bootout`. On Linux, uses `systemctl stop`.

## Uninstall

```bash
sprout service uninstall
sprout service uninstall -y   # Skip confirmation prompts
```

Before uninstalling, the command checks for active agent sessions on the running daemon. If active sessions are found, it warns:

```
Warning: 2 active agent session(s) detected. Uninstalling will stop the daemon and terminate these sessions.
Continue? [y/N]
```

Use `-y`/`--yes` to skip the prompt.

The uninstall removes:
- The service file (plist or systemd unit)
- The `service.env` file

Log files are **not** removed.

## Log Files

The daemon writes logs to:

| File | Contents |
|---|---|
| `~/.local/state/sprout/logs/daemon.stdout.log` | Standard output (info messages, web server logs) |
| `~/.local/state/sprout/logs/daemon.stderr.log` | Standard error (warnings, errors) |
| `~/Library/Logs/sprout/daemon.log` (macOS) | launchd's stdout/stderr for the process: output before the daemon's own logging starts, and Go runtime crash traces |

The state-directory logs are rotated in-process via [lumberjack](https://github.com/natefinch/lumberjack) (files `0600`, directory `0700`):
- **Max size**: 10 MB per file
- **Max backups**: 5 rotated files
- **Compression**: enabled (`.gz`)

launchd does not rotate `~/Library/Logs/sprout/daemon.log`. It stays small because the daemon redirects its output to the rotated logs as soon as it starts; if a crash loop grows it, truncate it (`: > ~/Library/Logs/sprout/daemon.log`) or add a `newsyslog` rule in `/etc/newsyslog.d/`. The file is created `0600` in a `0700` directory. `sprout service status` and `sprout service diagnose` print both locations.

To watch logs in real-time:

```bash
tail -f ~/.local/state/sprout/logs/daemon.stdout.log
```

## Live Reload

The daemon supports live configuration reload via SIGHUP:

```bash
kill -HUP $(pgrep -f "sprout agent -d")
```

This re-reads the on-disk configuration and API keys without restarting the process. Running agents and active queries are **not** affected — only subsequent queries will use the new configuration.

**Output on SIGHUP:**

```
[RELOAD] Received SIGHUP, reloading configuration...
[RELOAD] Configuration reloaded successfully.
```

## Diagnose

```bash
sprout service diagnose
```

Checks the service installation for common issues:
- Service file exists and is current (macOS: flags a plist that stores `*_API_KEY` / `*_TOKEN` variables in plaintext, by name only, and tells you to re-run `sprout service install`)
- Daemon is loaded and running
- `service.env` is populated (variable names only)
- Log files and their locations

## Troubleshooting

### Daemon won't start

1. Check logs: `tail -20 ~/.local/state/sprout/logs/daemon.stderr.log` (macOS startup failures: `~/Library/Logs/sprout/daemon.log`)
2. Check which keys are captured: `sprout service diagnose` (prints names, not values)
3. Run diagnostics: `sprout service diagnose`
4. Try a clean reinstall:
   ```bash
   sprout service uninstall -y
   sprout service install
   ```

### Port already in use

The daemon listens on port 56000 by default. If another process is using it:

```bash
lsof -i :56000
```

### macOS: launchctl errors

- `Service already loaded`: Already installed. Use `sprout service start` or `sprout service uninstall` first.
- `Bootout failed`: Service not running. Usually safe to ignore.

### Linux: systemctl errors

- `Failed to connect to bus`: The systemd user instance isn't running. This is common in containers or SSH sessions without `enable-linger`.
  ```bash
  # Enable lingering (persistent user systemd)
  loginctl enable-linger $(whoami)
  ```

### Logs growing too large

Log rotation is automatic (10 MB, 5 backups, compressed). If logs are still too large, manually rotate:

```bash
# Truncate current log (daemon keeps writing)
> ~/.local/state/sprout/logs/daemon.stdout.log
```

## Security Model

### User-UID Execution

The daemon runs as the installing user (no root privileges). On macOS, this is enforced by launchd's per-user `~/Library/LaunchAgents/` domain. On Linux, systemd user units (`~/.config/systemd/user/`) run under the user's UID.

### Localhost-Only Default

By default, the web UI binds to `127.0.0.1:56000`, accepting connections only from the local machine. This is the default when no bind address is specified.

### Auth Token Requirement for Remote Access

To expose the web UI on a non-localhost address, you **must** set the `SPROUT_AUTH_TOKEN` environment variable. The daemon will refuse to start if configured to bind to a public interface without authentication.

```bash
# In service.env (~/.local/state/sprout/service.env)
SPROUT_AUTH_TOKEN=your-secret-token-here
SPROUT_BIND_ADDR=0.0.0.0
```

When `SPROUT_AUTH_TOKEN` is set, all write endpoints (query, file operations, terminal) require the token in the `Authorization: Bearer <token>` header. Read-only endpoints (static assets, health checks) remain open.

### Environment File Permissions

`service.env` contains API keys and should be protected:

```bash
chmod 600 ~/.local/state/sprout/service.env
```

The install command creates this file with `0600` permissions. The launchd plist and systemd unit contain no secrets; the plist is still written `0600`.
