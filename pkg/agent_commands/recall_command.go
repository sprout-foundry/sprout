package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// RecallCommand implements the /recall slash command. It searches past
// sessions for content matching the given query and renders the results.
type RecallCommand struct{}

// Name returns the slash command name.
func (c *RecallCommand) Name() string {
	return "recall"
}

// Description returns the short description shown in /help listings.
func (c *RecallCommand) Description() string {
	return "Search past sessions for relevant context"
}

// Usage returns the long help string shown by /help recall.
func (c *RecallCommand) Usage() string {
	return strings.Join([]string{
		"Search prior sessions for content matching the query.",
		"",
		"Usage: /recall <query> [flags]",
		"",
		"Flags:",
		"  --limit <N>    Maximum number of items (default 5)",
		"  --json         Emit raw JSON array of recalled items",
		"",
		"Examples:",
		`  /recall "auth error"`,
		`  /recall --limit 10 "deployment rollback"`,
		`  /recall --json "rate limit"`,
	}, "\n")
}

// parseRecallFlags extracts --limit from raw args (--json is stripped by
// the registry before Execute is called). Returns the limit (default 5)
// and the query (joined from non-flag tokens).
func parseRecallFlags(args []string) (limit int, query string, err error) {
	limit = 5
	var queryParts []string
	i := 0
	for i < len(args) {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			queryParts = append(queryParts, arg)
			i++
			continue
		}
		switch arg {
		case "--limit":
			if i+1 >= len(args) {
				return 0, "", errors.New("--limit requires a numeric value")
			}
			i++
			n, perr := strconv.Atoi(args[i])
			if perr != nil {
				return 0, "", fmt.Errorf("--limit: invalid integer %q", args[i])
			}
			if n <= 0 {
				return 0, "", fmt.Errorf("--limit: must be positive, got %d", n)
			}
			limit = n
			i++
		default:
			return 0, "", fmt.Errorf("unknown flag %q", arg)
		}
	}
	return limit, strings.Join(queryParts, " "), nil
}

// runRecall calls agent.Recall and returns the items. Shared between
// Execute (text) and ExecuteWithJSONOutput (JSON).
func runRecall(args []string, chatAgent *agent.Agent) ([]agent.RecalledItem, error) {
	if chatAgent == nil {
		return nil, errors.New("agent not available")
	}
	limit, query, err := parseRecallFlags(args)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("usage: /recall <query> [--limit <N>] [--json]")
	}
	items, err := chatAgent.Recall(chatAgent.InterruptCtx(), query, limit)
	if err != nil {
		return nil, fmt.Errorf("recall failed: %w", err)
	}
	if items == nil {
		items = []agent.RecalledItem{}
	}
	return items, nil
}

// Execute renders the recalled items as a friendly markdown block.
func (c *RecallCommand) Execute(args []string, chatAgent *agent.Agent) error {
	items, err := runRecall(args, chatAgent)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Printf("No prior sessions match %q.\n", joinQueryFromArgs(args))
		return nil
	}
	// FormatSemanticRecall provides its own "Recalled From Session History"
	// header, so we just emit it directly without a redundant prefix.
	WriteToOutput(agent.FormatSemanticRecall(items, 8000))
	return nil
}

// ExecuteWithJSONOutput emits the raw []RecalledItem slice as JSON.
func (c *RecallCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	items, err := runRecall(args, chatAgent)
	if err != nil {
		return err
	}
	return WriteJSONToOutput(items)
}

// joinQueryFromArgs extracts the query string portion from raw args
// (used by Execute when rendering the empty-results message).
func joinQueryFromArgs(args []string) string {
	var parts []string
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--limit" {
			skipNext = true
			continue
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}
