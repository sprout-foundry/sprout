//go:build !js

// Simple enhanced agent command with web UI support
// Flag variables for web UI configuration (used by agent_modes.go)
package cmd

import (
	commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils"
	"github.com/sprout-foundry/sprout/pkg/webui"
	"github.com/sprout-foundry/sprout/pkg/zsh"
)

var (
	disableWebUI    bool
	noProjectSkills bool
	webPort         int
	webBindAddr     string
	daemonMode      bool
	bindSocket      string
	secretToken     string
)

func init() {
	agentCmd.Flags().BoolVar(&disableWebUI, "no-web-ui", false, "Disable web UI")
	agentCmd.Flags().BoolVar(&noProjectSkills, "no-project-skills", false, "Skip discovery of project-local skills from .sprout/skills/")
	agentCmd.Flags().IntVar(&webPort, "web-port", 0, "Port for web UI (default: 56000 for daemon mode)")
	agentCmd.Flags().IntVar(&webPort, "port", 0, "") // Hidden alias for --web-port (Docker/cloud entrypoint compat)
	agentCmd.Flags().MarkHidden("port")
	agentCmd.Flags().StringVar(&webBindAddr, "bind", "", "Bind address for web UI (default: 127.0.0.1, or set SPROUT_BIND_ADDR)")
	agentCmd.Flags().BoolVarP(&daemonMode, "daemon", "d", false, "Run in daemon mode - keep web UI running without interactive prompt")
	agentCmd.Flags().StringVar(&bindSocket, "bind-socket", "", "Listen on a Unix domain socket at path instead of TCP")
	agentCmd.Flags().StringVar(&secretToken, "secret", "", "Auth token for write endpoints (alternative to SPROUT_AUTH_TOKEN env var)")
}

// Ensure imports are used
var (
	_ = commands.NewCommandRegistry
	_ = console.NewCIOutputHandler
	_ = events.NewEventBus
	_ = utils.GetLogger
	_ = webui.NewReactWebServer
	_ = zsh.IsCommand
)
