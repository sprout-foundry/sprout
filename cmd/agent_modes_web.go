//go:build !js

package cmd

// setupWebUIServer resolves the bind address, applies the port strategy
// (daemon single-port supervisor vs non-daemon dynamic port), builds and
// starts the web UI server with all of its agent wiring, and — in daemon
// mode — starts the agent socket server and the idle reaper.
// It returns the started server, the supervisor (nil outside daemon mode),
// the resolved bind address, a cleanup that closes the daemon socket
// servers, and an error (only the bind-address validation can fail;
// server-start failures are fatal via log.Fatalf, as in the original).
// Split out of RunAgent (agent_modes.go).
import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/webui"
)

func setupWebUIServer(ctx context.Context, cancel context.CancelFunc, chatAgent *agent.Agent, eventBus *events.EventBus, enableWebUI bool) (*webui.ReactWebServer, *webUISupervisor, string, func(), error) {
	// Create web server if enabled
	var webServer *webui.ReactWebServer
	var webUISup *webUISupervisor
	var cleanups []func() error

	// Resolve bind address early so it's available in all code paths.
	// --bind flag → SPROUT_BIND_ADDR env var → "127.0.0.1" default
	bindAddr := webBindAddr
	if bindAddr == "" {
		bindAddr = configuration.GetEnvSimple("BIND_ADDR")
	}
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}

	// Validate the bind address is a plausible IP or hostname.
	if bindAddr != "localhost" && net.ParseIP(bindAddr) == nil {
		return nil, nil, "", nil, fmt.Errorf("invalid bind address %q: must be a valid IP address", bindAddr)
	}

	if enableWebUI {
		// Warn when binding to all interfaces
		if bindAddr == "0.0.0.0" || bindAddr == "::" {
			console.GlyphWarning.Fprintf(os.Stderr, "Binding to %s — web UI is accessible from all network interfaces", bindAddr)
		}

		// Determine port strategy.
		//
		// IN VARIANT: The daemon serves ALL workspaces from a single port.
		//   When daemon mode is active and no explicit --web-port is given,
		//   the port is always DaemonPort (56000).  The daemon handles
		//   multiple workspaces by routing internally per-workspace via
		//   clientContext / chat_sessions — there are NO folder-scoped
		//   daemon ports.  The single-port supervisor handles leadership
		//   election so exactly one daemon process serves 56000; additional
		//   daemon instances attach to the leader.
		//
		// Non-daemon interactive (no explicit port): each instance gets its
		// own dynamic port (starting at 56001 = DaemonPort+1) so that
		// separate browser windows can connect independently.  This is
		// intentional: non-daemon instances are short-lived and do not share
		// a persistent port.
		//
		// Explicit --web-port N: always start directly on that port,
		// regardless of daemon mode (user override).
		//
		// GUARD: daemon mode NEVER falls into the FindAvailablePort path.
		//   If daemonMode is true and webPort is 0, the port is set to
		//   webui.DaemonPort unconditionally below.
		port := webPort
		if port == 0 {
			if daemonMode {
				// Daemon mode: always use the single shared DaemonPort.
				// This path is mutually exclusive with the dynamic-port branch
				// — if daemonMode is true, FindAvailablePort is never called.
				port = webui.DaemonPort
			} else {
				// Non-daemon interactive: find a free dynamic port so each
				// instance gets its own browser window.  This path is only
				// reachable when daemonMode is false.
				dynamicPort, dynErr := webui.FindAvailablePort(webui.DaemonPort + 1)
				if dynErr != nil {
					console.GlyphWarning.Fprintf(os.Stderr, "Could not find a dynamic port: %v; web UI disabled", dynErr)
					enableWebUI = false
				} else {
					port = dynamicPort
				}
			}
		}

		if enableWebUI {
			var webErr error
			// Seed the slash-command registry on the shared agent. In shared
			// mode the WebUI adopts this exact agent instance; the command
			// surface resolves commands through the agent's registry, and the
			// CLI's registry assignment normally happens later (interactive
			// REPL loop) which daemon mode never reaches — without this the
			// WebUI's /clear (New Session button) failed with
			// "command_not_found".
			if chatAgent != nil && chatAgent.SlashCommands() == nil {
				chatAgent.SetSlashCommands(agent_commands.NewCommandRegistry())
			}
			webServer, webErr = webui.NewReactWebServer(chatAgent, eventBus, port, bindAddr, bindSocket, secretToken)
			if webErr != nil {
				log.Fatalf("%v", webErr)
			}

			// SP-118 Phase 1: Route to Mode 1 (single-active-session) for the
			// sprout agent path (interactive / direct / non-daemon). The daemon
			// path (daemonMode=true) leaves agentEnforceSingleSession=false so
			// connections route to the Mode 2 stub until SP-118-2 lands. The
			// stub logs and drops connections, so daemon mode's WebUI is
			// intentionally broken in this phase. The flag is the dispatch
			// signal — NOT serviceMode, which tests manipulate independently
			// to exercise Mode 1 in service-mode setups.
			if !daemonMode {
				webServer.SetAgentEnforceSingleSession(true)
			}

			// In shared mode, register the server so the CLI's ProcessQuery
			// wrapper can sync agent state after each CLI query.
			if !daemonMode {
				setSharedWebServer(webServer)
			}

			// Inject webui-owned managers into the agent so that security
			// prompts and ask_user requests route through the same instances
			// the webui handlers resolve responses on — no global singletons.
			// Skip this when agent is nil (provider not configured in daemon mode).
			if chatAgent != nil {
				chatAgent.InjectWebUIManagers(webServer.GetSecurityPromptMgr(), webServer.GetAskUserMgr())

				// Wire up the WebUI client check so security prompts route
				// correctly: use the event bus only when a browser tab is open,
				// otherwise fall back to CLI prompting (avoids 5-min timeouts).
				chatAgent.SetHasActiveWebUIClients(webServer.HasActiveWebUIClients)

				// Register the password prompter mux so shell commands that trigger
				// sudo/passwd prompts route through the most appropriate surface:
				//   1. WebUI prompter (browser dialog) when a tab is open — best UX
				//   2. CLI prompter (terminal ReadPassword) when no tab is open
				//   3. Neither — sudo prompts hang as before (safe default)
				//
				// The mux is necessary because the agent_creation.go path sets a CLI
				// prompter unconditionally when stdin is a TTY. Setting only the WebUI
				// prompter here would clobber the CLI fallback and leave headless runs
				// with no prompt surface at all.
				if existing := chatAgent.GetPasswordPrompter(); existing != nil {
					chatAgent.SetPasswordPrompter(agent.NewCascadingPasswordPrompter(
						agent.NewWebUIPasswordPrompter(chatAgent),
						existing,
					))
				} else {
					chatAgent.SetPasswordPrompter(agent.NewWebUIPasswordPrompter(chatAgent))
				} // In shared mode (non-daemon interactive), seed the agent's
				// event metadata with the default client/chat IDs so that
				// CLI-initiated queries publish events the WebUI can route.
				// Without this, CLI events lack client_id/chat_id and the
				// WebUI tab never receives streaming output or completion
				// notifications for CLI queries.
				if !daemonMode {
					chatAgent.SetEventMetadata(map[string]interface{}{
						"client_id": "default",
						"chat_id":   "default",
					})
				}
			}

			startInstanceTracker(ctx, port, chatAgent)

			// Daemon mode without explicit port → single-port supervisor.
			if webPort == 0 && daemonMode {
				webUISup = newWebUISupervisor(
					webServer,
					port,
					func(activePort int) {
						setWebUIDisplayURL(fmt.Sprintf("http://%s:%d", webui.DisplayAddr(bindAddr), activePort))
						console.GlyphInfo.Printf("Web UI available at http://%s:%d\n", webui.DisplayAddr(bindAddr), activePort)
					},
					func(activePort int) {
						setWebUIDisplayURL(fmt.Sprintf("http://%s:%d", webui.DisplayAddr(bindAddr), activePort))
						console.GlyphInfo.Printf("Reusing active Web UI at http://%s:%d\n", webui.DisplayAddr(bindAddr), activePort)
					},
				)
				go webUISup.Run(ctx)

				// Wait for web server to start running before proceeding
				startupDeadline := time.NewTimer(5 * time.Second)
				defer startupDeadline.Stop()
				startupPoll := time.NewTicker(50 * time.Millisecond)
				defer startupPoll.Stop()

			daemonStartupLoop:
				for {
					if webServer.IsRunning() || webUISup.HasAttached() {
						break
					}

					select {
					case <-startupDeadline.C:
						if !webServer.IsRunning() && !webUISup.HasAttached() {
							return nil, nil, bindAddr, nil, fmt.Errorf("web UI failed to start on port %d (daemon mode)", port)
						}
						break daemonStartupLoop
					case <-startupPoll.C:
					}
				}
			} else {
				// Explicit port OR non-daemon dynamic port: start directly.
				startErrCh := make(chan error, 1)
				go func() {
					if err := webServer.Start(ctx); err != nil && ctx.Err() == nil {
						select {
						case startErrCh <- err:
						default:
						}
						console.GlyphWarning.Fprintf(os.Stderr, "Web UI failed to start: %v", err)
					}
				}()

				startupDeadline := time.NewTimer(1500 * time.Millisecond)
				defer startupDeadline.Stop()
				startupPoll := time.NewTicker(50 * time.Millisecond)
				defer startupPoll.Stop()

			loop:
				for {
					if webServer.IsRunning() {
						break
					}

					select {
					case startErr := <-startErrCh:
						return nil, nil, bindAddr, nil, fmt.Errorf("web UI failed to start on port %d: %w", port, startErr)
					case <-startupDeadline.C:
						if !webServer.IsRunning() {
							return nil, nil, bindAddr, nil, fmt.Errorf("web UI failed to start on port %d", port)
						}
						break loop
					case <-startupPoll.C:
					}
				}

				setWebUIDisplayURL(fmt.Sprintf("http://%s:%d", webui.DisplayAddr(bindAddr), webServer.GetPort()))
				console.GlyphInfo.Printf("Web UI available at http://%s:%d\n", webui.DisplayAddr(bindAddr), webServer.GetPort())
			}
		}

		var socketActivities []*daemon.DaemonActivity

		// SP-136 P4: the daemon hosts the agent socket so the CLI can run
		// one-shot queries through the daemon-owned agent.
		if daemonMode {
			agentSrv := startDaemonAgentServer(ctx, true, chatAgent)
			if agentSrv != nil {
				cleanups = append(cleanups, agentSrv.Close)
				socketActivities = append(socketActivities, agentSrv.Activity)
			}
		}

		// SP-136 P2: idle reaping for auto-started daemons.
		// When SPROUT_DAEMON_IDLE_TIMEOUT is a positive duration, the daemon
		// self-terminates after the web UI has had no active clients and no
		// active queries — and no socket traffic — for that long. Auto-start
		// (cmd/daemon_autostart.go) sets this on daemons it spawns;
		// explicitly-started daemons (sprout agent -d) are unaffected unless
		// the operator opts in.
		if daemonMode && webServer != nil {
			if idleTimeout, perr := time.ParseDuration(os.Getenv("SPROUT_DAEMON_IDLE_TIMEOUT")); perr == nil && idleTimeout > 0 {
				go reapIdleDaemon(ctx, cancel, webServer, socketActivities, idleTimeout)
			}
		}
	}

	// The socket servers (when daemon mode created them) outlive this
	// function — the caller defers the cleanup so they close at RunAgent
	// exit, exactly as the original in-block defers did. Run LIFO to match
	// the original defer order.
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	return webServer, webUISup, bindAddr, cleanup, nil
}
