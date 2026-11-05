//go:build !js

package cmd

import (
	"context"
	"fmt"
	"time"

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
	// A host that bounds the run (a task VM) passes its deadline; the agent
	// is told to wrap up shortly before it rather than being cut off mid-work,
	// and its background sessions are stopped then and when the run ends, so
	// the machine is quiet for the next run.
	defer startTaskRun(chatAgent, backgroundStopper(chatAgent), time.Now())()

	// Slash/bang commands should bypass command-detection fast paths.
	registry := agent_commands.DefaultRegistry()
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

	// Proceed with normal agent flow
	err := ProcessQuery(ctx, chatAgent, eventBus, query)
	if q := chatAgent.PendingQuestion(); q != nil {
		// The run stopped to ask (ask_user_ends_run): a clean end, not a
		// failure; the structured result carries the question.
		fmt.Printf("%sStopped to ask: %s\n", console.GlyphInfo.Prefix(), q.Question)
		return nil
	}
	return err
}
