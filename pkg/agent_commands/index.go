package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// IndexCommand implements the /index slash command for toggling workspace indexing.
type IndexCommand struct{}

// Name returns the command name
func (c *IndexCommand) Name() string {
	return "index"
}

// Description returns the command description
func (c *IndexCommand) Description() string {
	return "Toggle workspace indexing on/off for semantic search and duplicate detection"
}

// Usage returns the detailed help text shown by `/help index`.
func (c *IndexCommand) Usage() string {
	return strings.Join([]string{
		"/index                Toggle workspace indexing on or off.",
		"/index on|enable      Turn indexing on (builds in background).",
		"/index off|disable    Turn indexing off (preserves existing data).",
		"/index status         Show current indexing status and record count.",
		"",
		"Indexing powers semantic search (/search) and duplicate detection.",
	}, "\n")
}

// Execute runs the index command
func (c *IndexCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	action := "toggle"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}

	switch action {
	case "on", "enable":
		return c.enableIndex(chatAgent)
	case "off", "disable":
		return c.disableIndex(chatAgent)
	case "status":
		return c.showStatus(chatAgent)
	case "toggle", "":
		// Toggle based on current state
		if chatAgent.IsEmbeddingIndexEnabled() {
			return c.disableIndex(chatAgent)
		}
		return c.enableIndex(chatAgent)
	default:
		return fmt.Errorf("unknown action %q; use 'on', 'off', 'status', or no args to toggle", action)
	}
}

func (c *IndexCommand) enableIndex(chatAgent *agent.Agent) error {
	if chatAgent.IsEmbeddingIndexEnabled() {
		fmt.Println("\n[index] Indexing is already enabled for this workspace.")
		return nil
	}

	if err := chatAgent.EnableEmbeddingIndex(); err != nil {
		return fmt.Errorf("failed to enable indexing: %w", err)
	}

	fmt.Println("\n[index] Workspace indexing enabled.")
	fmt.Println("       Building index in the background...")
	fmt.Println("       Semantic search and duplicate detection are now available.")
	return nil
}

func (c *IndexCommand) disableIndex(chatAgent *agent.Agent) error {
	if !chatAgent.IsEmbeddingIndexEnabled() {
		fmt.Println("\n[index] Indexing is not currently enabled for this workspace.")
		return nil
	}

	chatAgent.DisableEmbeddingIndex()
	fmt.Println("\n[index] Workspace indexing disabled.")
	fmt.Println("       The index has been stopped. Existing index data is preserved.")
	return nil
}

func (c *IndexCommand) showStatus(chatAgent *agent.Agent) error {
	enabled := chatAgent.IsEmbeddingIndexEnabled()
	fmt.Printf("\n[index] Status: %s\n", map[bool]string{true: "ENABLED", false: "DISABLED"}[enabled])

	em := chatAgent.GetEmbeddingManager()
	if em != nil {
		fmt.Printf("       Index records: %d\n", em.IndexSize())
		fmt.Printf("       Initialized: %v\n", em.IsInitialized())
	} else {
		fmt.Println("       No index data.")
	}
	return nil
}
