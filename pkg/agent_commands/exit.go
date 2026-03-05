package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
)

var exitProcess = os.Exit

// ExitCommand implements the /exit slash command
type ExitCommand struct{}

// Name returns the command name
func (e *ExitCommand) Name() string {
	return "exit"
}

// Description returns the command description
func (e *ExitCommand) Description() string {
	return "Exit the interactive session"
}

// Execute runs the exit command
func (e *ExitCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// Print full session summary before exiting
	fmt.Println("\n👋 Goodbye! Here's your session summary:")
	fmt.Println("=====================================")
	chatAgent.PrintConversationSummary(true)
	sessionID := strings.TrimSpace(chatAgent.GetSessionID())
	if sessionID == "" {
		sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
		chatAgent.SetSessionID(sessionID)
	}
	fmt.Printf("To Continue: `ledit agent --session-id %s`\n", sessionID)
	fmt.Println("Or Resume Latest: `ledit agent --last-session`")
	exitProcess(0)
	return nil // This line won't be reached due to os.Exit
}
