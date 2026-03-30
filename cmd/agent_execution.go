// Simple enhanced agent command with web UI support
// Flag variables for web UI configuration (used by agent_modes.go)
package cmd

import (
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webui"
	"github.com/alantheprice/ledit/pkg/zsh"
)

var (
	disableWebUI bool
	webPort      int
	daemonMode   bool
)

func init() {
	agentCmd.Flags().BoolVar(&disableWebUI, "no-web-ui", false, "Disable web UI")
	agentCmd.Flags().IntVar(&webPort, "web-port", 0, "Port for web UI (default: 54000 for daemon mode)")
	agentCmd.Flags().BoolVarP(&daemonMode, "daemon", "d", false, "Run in daemon mode - keep web UI running without interactive prompt")
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
