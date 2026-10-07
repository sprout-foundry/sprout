//go:build !js

package webui

// server_constructor.go — ReactWebServer construction, split out of
// server.go. NewReactWebServer wires up the server (daemon-root resolution,
// allowed-origins normalization, auth-token validation, manager/subscriber
// initialization); isLocalhostAddr is the bind-address safety guard it
// consults to refuse public exposure without an auth token.
import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/preview"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int, bindAddr string, socketPath string, authToken string) (*ReactWebServer, error) {
	// Socket mode is mutually exclusive with TCP
	if socketPath != "" {
		// In socket mode, port and bindAddr are irrelevant
		port = 0
		bindAddr = ""
	} else {
		if port == 0 {
			port = DaemonPort
		}
		if bindAddr == "" {
			bindAddr = "127.0.0.1"
		}
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	workspaceRoot, err = filepathAbsEval(workspaceRoot)
	if err != nil {
		workspaceRoot = "."
	}

	// daemonRoot is the user's home directory — it scopes daemon-level
	// storage (sessions, SSH tunnels, config) AND the workspace browser
	// (handleAPIWorkspaceBrowse) to the user rather than a specific project.
	//
	// Resolution, most authoritative first:
	//   1. SPROUT_DAEMON_ROOT — baked into the launchd/systemd unit at install
	//      time when $HOME is reliable. Source of truth for managed services.
	//   2. os.UserHomeDir() ($HOME) — the normal interactive case.
	//   3. user.Current().HomeDir (`/etc/passwd`) — bypasses $HOME, used in
	//      service mode when the plist/unit is stale and $HOME may be wrong
	//      (e.g., a launchd cache from a pre-SPROUT_DAEMON_ROOT install).
	//   4. workspaceRoot (the CWD) — last resort only.
	//
	// Falling back to the CWD is dangerous under a service manager: launchd /
	// systemd can start the daemon in "/" or another system dir, and because
	// the workspace browser is scoped to daemonRoot the user would be unable
	// to reach their projects. Warn loudly when we have to fall back.
	serviceMode := configuration.GetEnvSimple("SERVICE") == "1"
	daemonRoot := configuration.GetEnvSimple("DAEMON_ROOT")
	rootSource := "SPROUT_DAEMON_ROOT"
	if daemonRoot == "" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
			daemonRoot = home
			rootSource = "$HOME"
		}
	}
	// In service mode, always reconcile against the OS user database.
	// SPROUT_DAEMON_ROOT is baked into the plist/unit at install time and can
	// go stale — e.g. a unit generated on Linux (/home/user) copied to macOS
	// where the real home is /Users/user, or a renamed user account. The
	// previous looksLikeUserHome heuristic failed because /home/user passes
	// both the heuristic AND the disk-existence check on macOS (synthetic
	// firmlinks). user.Current().HomeDir reads /etc/passwd (or its platform
	// equivalent), which is always authoritative for the current user.
	if serviceMode {
		if u, uErr := user.Current(); uErr == nil && u.HomeDir != "" && u.HomeDir != daemonRoot {
			webuiLogger.Warn("configured daemon root differs from current user home; using current user home",
				slog.String("configured_daemon_root", daemonRoot),
				slog.String("current_user_home", u.HomeDir),
				slog.String("selected_daemon_root", u.HomeDir),
				slog.String("remediation", "sprout service uninstall && sprout service install"),
			)
			daemonRoot = u.HomeDir
			rootSource = "user.Current().HomeDir"
		}
	}
	if daemonRoot == "" {
		daemonRoot = workspaceRoot
		if serviceMode {
			webuiLogger.Warn("user home could not be resolved; workspace browser may not reach projects",
				slog.String("workspace_root", workspaceRoot),
				slog.String("remediation", "sprout service uninstall && sprout service install"),
			)
		}
	}

	webuiLogger.Info("web UI startup configuration resolved", slog.String("workspace_root", workspaceRoot), slog.String("daemon_root", daemonRoot), slog.Bool("service_mode", serviceMode), slog.String("daemon_root_source", rootSource))

	// Resolve daemonRoot symlinks early so the recent-workspace check below
	// compares canonical paths (recent-workspace paths are stored after
	// filepathAbsEval → EvalSymlinks). Without this, /var/folders vs
	// /private/var/folders on macOS produces false negatives.
	resolvedDaemonRoot := daemonRoot
	if evaled, err := filepath.EvalSymlinks(daemonRoot); err == nil {
		resolvedDaemonRoot = evaled
	}

	if serviceMode {
		// SP-130: in service mode the daemon's CWD is typically $HOME (baked
		// into the plist/unit), so blindly defaulting the workspace to the
		// CWD/daemonRoot means the agent runs with the entire home directory
		// in scope. Instead, try to restore the most recent valid workspace.
		// If it exists and is within daemonRoot, use it; otherwise leave
		// workspaceRoot as the CWD-derived default (home) and let the
		// frontend gate force explicit selection.
		if recent := GetMostRecentWorkspace(); recent != "" {
			if info, err := os.Stat(recent); err == nil && info.IsDir() {
				if abs, err := filepath.Abs(recent); err == nil {
					if isWithinWorkspace(abs, resolvedDaemonRoot) {
						workspaceRoot = abs
					}
				}
			}
		}
		// If no valid recent workspace was found, workspaceRoot stays as the
		// CWD-derived default — the frontend gate catches home and forces
		// explicit selection rather than silently running scoped to ~.
	}

	// Initialize recent workspace tracking.
	initRecentWorkspaces()

	providercatalog.RefreshFromRemoteAsync("")

	securityPromptMgr := security.NewApprovalManager()

	askUserMgr := agenttools.NewAskUserManager()

	// Run startup permission check
	if configDir, err := configuration.GetConfigDir(); err == nil {
		// Check for symlinks pointing outside the config directory
		symlinkWarnings := security.CheckAllSymlinks(configDir)
		if len(symlinkWarnings) > 0 {
			webuiLogger.Warn("configuration symlink warnings detected")
			for _, warn := range symlinkWarnings {
				webuiLogger.Warn("configuration symlink warning", slog.String("warning", warn))
			}
		}

		// Run the full permission check
		security.RunStartupCheck(configDir)
	}

	// Parse allowed origins from SPROUT_ALLOWED_ORIGINS env var
	// This is a comma-separated list of origin URLs to allow.
	// Origins are pre-normalized at startup so CheckOrigin can do
	// simple string comparisons without re-parsing on every request.
	allowedOriginsStr := strings.TrimSpace(configuration.GetEnvSimple("ALLOWED_ORIGINS"))
	var normalizedAllowedOrigins []string
	if allowedOriginsStr != "" {
		parts := strings.Split(allowedOriginsStr, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				parsed, err := url.Parse(trimmed)
				if err != nil {
					webuiLogger.Warn("skipping malformed allowed origin", slog.String("origin", trimmed), slog.Any("err", err))
					continue
				}
				normalizedAllowedOrigins = append(normalizedAllowedOrigins, normalizeOriginForCompare(parsed))
			}
		}
	}
	if len(normalizedAllowedOrigins) > 0 {
		webuiLogger.Info("allowed origins configured", slog.Any("allowed_origins", normalizedAllowedOrigins))
	}

	// Parse trusted user header (serviceMode already resolved above)
	trustedUserHeader := strings.TrimSpace(configuration.GetEnvSimple("TRUSTED_USER_HEADER"))
	if serviceMode {
		if trustedUserHeader != "" {
			webuiLogger.Info("trusted user header configured", slog.String("header", trustedUserHeader), slog.Bool("service_mode", true))
		} else {
			webuiLogger.Warn("service mode enabled without a trusted user header")
		}
	}

	// Parse auth token: explicit parameter takes precedence over env var
	resolvedAuthToken := authToken
	if resolvedAuthToken == "" {
		resolvedAuthToken = strings.TrimSpace(configuration.GetEnvSimple("AUTH_TOKEN"))
	}
	if resolvedAuthToken != "" {
		webuiLogger.Info("auth token configured; write endpoints require authentication")
	}

	// Security: refuse to start if bound to a non-localhost address without
	// an auth token.  Exposing the web UI on a public interface without any
	// authentication is a serious security risk.
	// Skip this check for Unix socket mode (socketPath is non-empty) since
	// Unix sockets are inherently local-only.
	if socketPath == "" && !isLocalhostAddr(bindAddr) && resolvedAuthToken == "" {
		return nil, fmt.Errorf("Refusing to start: SPROUT_BIND_ADDR=%s requires SPROUT_AUTH_TOKEN to be set.", bindAddr)
	}

	// Resolve symlinks on both roots so that path comparisons are consistent
	// (macOS /var → /private/var is the common case; without this, any path
	// that goes through filepath.EvalSymlinks will fail prefix checks).
	if evaled, err := filepath.EvalSymlinks(daemonRoot); err == nil {
		daemonRoot = evaled
	}
	if evaled, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		workspaceRoot = evaled
	}

	return &ReactWebServer{
		logger:            webuiLogger,
		agent:             agent,
		eventBus:          eventBus,
		daemonRoot:        daemonRoot,
		workspaceRoot:     workspaceRoot,
		sshHostAlias:      strings.TrimSpace(configuration.GetEnvSimple("SSH_HOST_ALIAS")),
		sshSessionKey:     strings.TrimSpace(configuration.GetEnvSimple("SSH_SESSION_KEY")),
		sshLauncherURL:    strings.TrimSpace(configuration.GetEnvSimple("SSH_LAUNCHER_URL")),
		sshHomePath:       strings.TrimSpace(configuration.GetEnvSimple("SSH_HOME")),
		fileConsents:      newFileConsentManager(),
		fileWatcher:       newFileWatcher(eventBus),
		securityPromptMgr: securityPromptMgr,
		askUserMgr:        askUserMgr,
		clientContexts:    make(map[string]*webClientContext),
		clientWorkspaces:  make(map[string]string),
		chatSubscribers:   newChatSubscribersRegistry(),
		userConnections:   &UserConnections{},
		port:              port,
		bindAddr:          bindAddr,
		socketPath:        socketPath,
		upgrader: websocket.Upgrader{
			CheckOrigin: newCheckOriginFunc(bindAddr, normalizedAllowedOrigins),
		},
		terminalManager:          NewTerminalManager(workspaceRoot),
		startTime:                time.Now(),
		fixReviewJobs:            make(map[string]*gitFixReviewJob),
		previewManagers:          make(map[string]*preview.Manager),
		sshSessions:              make(map[string]*sshWorkspaceSession),
		sshInFlight:              make(map[string]chan struct{}),
		sshLaunchStatuses:        make(map[string]*sshLaunchStatus),
		normalizedAllowedOrigins: normalizedAllowedOrigins,
		trustedUserHeader:        trustedUserHeader,
		serviceMode:              serviceMode,
		authToken:                resolvedAuthToken,
	}, nil
}

// isLocalhostAddr returns true if the given bind address is a safe local-only
// address that cannot be reached from external networks.
func isLocalhostAddr(addr string) bool {
	switch addr {
	case "", "127.0.0.1", "localhost", "[::1]", "::1":
		return true
	}
	return false
}
