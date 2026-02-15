// Simple enhanced agent command with web UI support
// Flag variables for web UI configuration (used by agent_modes.go)
package cmd

import (
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/security_validator"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webui"
	"github.com/alantheprice/ledit/pkg/zsh"
)

var (
	disableWebUI bool
	webPort      int
)

func init() {
	agentCmd.Flags().BoolVar(&disableWebUI, "no-web-ui", false, "Disable web UI")
	agentCmd.Flags().IntVar(&webPort, "web-port", 0, "Port for web UI (default: auto-find available port starting from 54321)")
}

// Ensure imports are used
var (
	_ = commands.NewCommandRegistry
	_ = configuration.SecurityValidationConfig{}
	_ = console.NewCIOutputHandler
	_ = events.NewEventBus
	_ = security_validator.NewValidator
	_ = utils.GetLogger
	_ = webui.NewReactWebServer
	_ = zsh.IsCommand
)
