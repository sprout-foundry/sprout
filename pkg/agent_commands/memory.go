package commands

import (
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
)

// MemoryCommand handles memory features with auto-tracking and session recovery
type MemoryCommand struct{}

func (c *MemoryCommand) Name() string {
	return "memory"
}

func (c *MemoryCommand) Description() string {
	return "Manage conversation memory - show summary, list sessions, load session, or clear memory"
}

func (c *MemoryCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /memory [summary|list|load|clear|delete]")
	}

	subcommand := args[0]
	switch subcommand {
	case "summary":
		return c.showSummary(chatAgent)
	case "list":
		return c.listSessions()
	case "load":
		if len(args) < 2 {
			return fmt.Errorf("usage: /memory load <session_id>")
		}
		return c.loadSession(chatAgent, args[1])
	case "clear":
		return c.clearMemory(chatAgent)
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: /memory delete <session_id>")
		}
		return c.deleteSession(args[1])
	default:
		return fmt.Errorf("unknown subcommand: %s. Use: summary, list, load, clear, delete", subcommand)
	}
}

func (c *MemoryCommand) showSummary(chatAgent *agent.Agent) error {
	summary := chatAgent.GetPreviousSummary()
	if summary == "" {
		fmt.Println("No previous conversation memory available.")
		return nil
	}

	fmt.Println("=== Previous Conversation Memory ===")
	fmt.Println(summary)
	fmt.Println("====================================")
	return nil
}

func (c *MemoryCommand) clearMemory(chatAgent *agent.Agent) error {
	chatAgent.ClearConversationHistory()
	fmt.Println("✓ Conversation memory cleared")
	return nil
}

func (c *MemoryCommand) loadSession(chatAgent *agent.Agent, sessionID string) error {
	state, err := chatAgent.LoadState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session: %v", err)
	}

	chatAgent.ApplyState(state)
	fmt.Printf("✓ Conversation memory loaded for session: %s\n", sessionID)
	return nil
}

func (c *MemoryCommand) listSessions() error {
	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %v", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No saved sessions found.")
		return nil
	}

	fmt.Println("=== Available Sessions ===")
	for i, session := range sessions {
		age := time.Since(session.LastUpdated)
		fmt.Printf("%d. %s (last updated: %s ago)\n", i+1, session.SessionID, formatDuration(age))
	}
	fmt.Println("==========================")
	return nil
}

func (c *MemoryCommand) deleteSession(sessionID string) error {
	err := agent.DeleteSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %v", err)
	}

	fmt.Printf("✓ Session deleted: %s\n", sessionID)
	return nil
}

// formatDuration formats time.Duration into human-readable format
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}