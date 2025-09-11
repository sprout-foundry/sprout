package commands

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
)

// ContinuityCommand handles conversation continuity features
type ContinuityCommand struct{}

func (c *ContinuityCommand) Name() string {
	return "continuity"
}

func (c *ContinuityCommand) Description() string {
	return "Manage conversation continuity - show summary, clear history, or save/load state"
}

func (c *ContinuityCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /continuity [summary|clear|save|load]")
	}

	subcommand := args[0]
	switch subcommand {
	case "summary":
		return c.showSummary(chatAgent)
	case "clear":
		return c.clearHistory(chatAgent)
	case "save":
		if len(args) < 2 {
			return fmt.Errorf("usage: /continuity save <session_id>")
		}
		return c.saveState(chatAgent, args[1])
	case "load":
		if len(args) < 2 {
			return fmt.Errorf("usage: /continuity load <session_id>")
		}
		return c.loadState(chatAgent, args[1])
	case "list":
		return c.listSessions()
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: /continuity delete <session_id>")
		}
		return c.deleteSession(args[1])
	default:
		return fmt.Errorf("unknown subcommand: %s. Use: summary, clear, save, load, list, delete", subcommand)
	}
}

func (c *ContinuityCommand) showSummary(chatAgent *agent.Agent) error {
	summary := chatAgent.GetPreviousSummary()
	if summary == "" {
		fmt.Println("No previous conversation summary available.")
		return nil
	}

	fmt.Println("=== Previous Conversation Summary ===")
	fmt.Println(summary)
	fmt.Println("======================================")
	return nil
}

func (c *ContinuityCommand) clearHistory(chatAgent *agent.Agent) error {
	chatAgent.ClearConversationHistory()
	fmt.Println("✓ Conversation history cleared")
	return nil
}

func (c *ContinuityCommand) saveState(chatAgent *agent.Agent, sessionID string) error {
	err := chatAgent.SaveState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to save state: %v", err)
	}

	fmt.Printf("✓ Conversation state saved for session: %s\n", sessionID)
	return nil
}

func (c *ContinuityCommand) loadState(chatAgent *agent.Agent, sessionID string) error {
	state, err := chatAgent.LoadState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load state: %v", err)
	}

	chatAgent.ApplyState(state)
	fmt.Printf("✓ Conversation state loaded for session: %s\n", sessionID)
	return nil
}

func (c *ContinuityCommand) listSessions() error {
	sessions, err := agent.ListSessions()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %v", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No saved sessions found.")
		return nil
	}

	fmt.Println("=== Available Sessions ===")
	for i, session := range sessions {
		fmt.Printf("%d. %s\n", i+1, session)
	}
	fmt.Println("==========================")
	return nil
}

func (c *ContinuityCommand) deleteSession(sessionID string) error {
	err := agent.DeleteSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %v", err)
	}

	fmt.Printf("✓ Session deleted: %s\n", sessionID)
	return nil
}