package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

var exitProcess = os.Exit

// ExitCommand implements the /exit slash command
type ExitCommand struct{}

// Name returns the command name
func (e *ExitCommand) Name() string {
	return "exit"
}

// SafeDuringSteer returns false - /exit terminates session
func (e *ExitCommand) SafeDuringSteer() bool {
	return false
}

// Description returns the command description
func (e *ExitCommand) Description() string {
	return "Exit the interactive session"
}

// Usage returns the detailed help text shown by `/help exit`.
func (e *ExitCommand) Usage() string {
	return "/exit   End the interactive session (also prints a session summary).\n"
}

// Execute runs the exit command
func (e *ExitCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// In daemon mode, /exit should not kill the process — it would take
	// down the webui server and all sessions. Return an error so the
	// caller (webui or CLI) can handle it appropriately.
	if os.Getenv("SPROUT_DAEMON") != "" {
		return fmt.Errorf("/exit is not available in daemon mode — close the session from the WebUI instead")
	}

	// Print full session summary before exiting
	fmt.Println("\n-- Goodbye! Here's your session summary:")
	fmt.Println("=====================================")
	if chatAgent != nil {
		chatAgent.PrintConversationSummary(true)
		sessionID := strings.TrimSpace(chatAgent.GetSessionID())
		if sessionID == "" {
			sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
			chatAgent.SetSessionID(sessionID)
		}
		fmt.Printf("To Continue: `sprout agent --session-id %s`\n", sessionID)
		fmt.Println("Or Resume Latest: `sprout agent --last-session`")
	}
	exitProcess(0)
	return nil // unreachable — os.Exit(0)
}
