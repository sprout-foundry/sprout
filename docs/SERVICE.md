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

The `install` and `uninstall` subcommands support `-y`/`--yes` to skip confirmation prompts.

## Install

```bash
sprout service install
```

This command:

1. Detects the OS (macOS → launchd, Linux → systemd)
2. Generates a service configuration (launchd plist or systemd unit)
3. Captures current API keys from the environment into `~/.sprout/service.env`
4. Installs and starts the service

### What gets created

| OS | Service file | Log files |
|---|---|---|
| macOS | `~/Library/LaunchAgents/com.sprout.daemon.plist` | `~/.sprout/logs/daemon.stdout.log`, `~/.sprout/logs/daemon.stderr.log` |
| Linux | `~/.config/systemd/user/sprout.service` | `~/.sprout/logs/daemon.stdout.log`, `~/.sprout/logs/daemon.stderr.log` |

### Environment file

The install command writes API keys and other environment variables to `~/.sprout/service.env`. This file is loaded by the service manager when starting the daemon.

**File format** (line-delimited `KEY=VALUE`):

```
SPROUT_ANTHROPIC_API_KEY=sk-ant-...
SPROUT_OPENAI_API_KEY=sk-...
```

To update API keys after install, either:
- Edit `~/.sprout/service.env` directly, then restart (`sprout service stop && sprout service start`)
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
- The `~/.sprout/service.env` file

Log files in `~/.sprout/logs/` are **not** removed.

## Log Files

The daemon writes logs to:

| File | Contents |
|---|---|
| `~/.sprout/logs/daemon.stdout.log` | Standard output (info messages, web server logs) |
| `~/.sprout/logs/daemon.stderr.log` | Standard error (warnings, errors) |

Logs are automatically rotated via [lumberjack](https://github.com/natefinch/lumberjack):
- **Max size**: 10 MB per file
- **Max backups**: 5 rotated files
- **Compression**: enabled (`.gz`)

To watch logs in real-time:

```bash
tail -f ~/.sprout/logs/daemon.stdout.log
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
- Service file exists and is valid
- Daemon is running and reachable
- `service.env` is populated
- Log directory exists
- Port 56000 is listening

## Troubleshooting

### Daemon won't start

1. Check logs: `tail -20 ~/.sprout/logs/daemon.stderr.log`
2. Verify API keys: `cat ~/.sprout/service.env`
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
> ~/.sprout/logs/daemon.stdout.log
```

## Security Model

### User-UID Execution

The daemon runs as the installing user (no root privileges). On macOS, this is enforced by launchd's per-user `~/Library/LaunchAgents/` domain. On Linux, systemd user units (`~/.config/systemd/user/`) run under the user's UID.

### Localhost-Only Default

By default, the web UI binds to `127.0.0.1:56000`, accepting connections only from the local machine. This is the default when no bind address is specified.

### Auth Token Requirement for Remote Access

To expose the web UI on a non-localhost address, you **must** set the `SPROUT_AUTH_TOKEN` environment variable. The daemon will refuse to start if configured to bind to a public interface without authentication.

```bash
# In ~/.sprout/service.env
SPROUT_AUTH_TOKEN=your-secret-token-here
SPROUT_BIND_ADDR=0.0.0.0
```

When `SPROUT_AUTH_TOKEN` is set, all write endpoints (query, file operations, terminal) require the token in the `Authorization: Bearer <token>` header. Read-only endpoints (static assets, health checks) remain open.

### Environment File Permissions

`~/.sprout/service.env` contains API keys and should be protected:

```bash
chmod 600 ~/.sprout/service.env
```

The install command creates this file with `0600` permissions by default.
