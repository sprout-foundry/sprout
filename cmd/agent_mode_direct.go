//go:build !js

package cmd

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// runDirectMode handles single query execution
func runDirectMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	if configuration.GetEnvSimple("SUBAGENT") != "1" {
		fmt.Printf("%sProcessing: %s\n", console.GlyphAction.Prefix(), query)
	}

	// Slash/bang commands should bypass command-detection fast paths.
	registry := agent_commands.NewCommandRegistry()
	if registry.IsSlashCommand(query) {
		return ProcessQuery(ctx, chatAgent, eventBus, query)
	}

	// Try zsh command detection first
	if executed, err := TryZshCommandExecution(ctx, chatAgent, query); err != nil {
		return fmt.Errorf("zsh command execution failed: %w", err)
	} else if executed {
		// Command was executed directly, skip normal agent flow
		return nil
	}

	// Try LLM-based fast path: direct command execution
	if executed, err := TryDirectExecution(ctx, chatAgent, query); err != nil {
		return fmt.Errorf("direct command execution failed: %w", err)
	} else if executed {
		// Command was executed directly, skip normal agent flow
		return nil
	}

	// Proceed with normal agent flow
	return ProcessQuery(ctx, chatAgent, eventBus, query)
}
