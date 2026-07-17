package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/codegraph"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// CodegraphCommand implements the /codegraph slash command for managing
// the code intelligence graph: build, stats, and dead-code detection.
type CodegraphCommand struct{}

// Name returns the command name
func (c *CodegraphCommand) Name() string {
	return "codegraph"
}

// SafeDuringSteer returns true - /codegraph is read-only stats or independent build
func (c *CodegraphCommand) SafeDuringSteer() bool {
	return true
}

// Description returns the command description
func (c *CodegraphCommand) Description() string {
	return "Code intelligence graph: build, stats, dead-code"
}

// Usage returns detailed help text for the command
func (c *CodegraphCommand) Usage() string {
	return `Usage: /codegraph <subcommand>

Subcommands:
  build          Full re-index of all source files
  update         Incremental update of changed files only
  stats          Show node/edge/file counts
  help           Show this usage message

With no arguments, shows this help text.`
}

// Execute runs the codegraph command
func (c *CodegraphCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// Subcommand dispatch
	subcommand := ""
	if len(args) > 0 {
		subcommand = strings.ToLower(strings.TrimSpace(args[0]))
	}

	switch subcommand {
	case "build":
		return c.runBuild()
	case "update":
		return c.runUpdate()
	case "stats":
		return c.runStats()
	case "", "help":
		fmt.Fprintln(os.Stdout, console.GlyphInfo.Prefix()+c.Usage())
		return nil
	default:
		fmt.Fprintf(os.Stdout, "Unknown subcommand %q.\n\n", subcommand)
		fmt.Fprintln(os.Stdout, console.GlyphInfo.Prefix()+c.Usage())
		return nil
	}
}

// fileParser adapts ExtractCallsAndSymbols to the codegraph.FileParser signature.
func fileParser(path string, content []byte) ([]codegraph.Symbol, []codegraph.Edge, error) {
	sw, err := tools.ExtractCallsAndSymbols(path, content)
	if err != nil {
		return nil, nil, err
	}
	return sw.ToCodegraphSymbols(path)
}

// openStore opens the codegraph store at the default path.
// Returns (nil, nil) if the database file does not exist (not indexed yet).
func openStore() (*codegraph.SQLiteStore, error) {
	gitRoot, err := git.GetGitRootDir()
	if err != nil {
		return nil, nil
	}
	dbPath := filepath.Join(gitRoot, ".sprout", "codegraph.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}
	return codegraph.NewStore("")
}

// runBuild performs a full re-index of all source files.
func (c *CodegraphCommand) runBuild() error {
	fmt.Fprintln(os.Stdout, console.GlyphInfo.Prefix()+"Building code intelligence graph...")

	store, err := codegraph.NewStore("")
	if err != nil {
		return fmt.Errorf("failed to open codegraph store: %w", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := store.IndexAll(ctx, fileParser); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	stats := store.Stats()
	fmt.Fprintf(os.Stdout, "Code intelligence graph built: %d nodes, %d edges, %d files\n",
		stats.NodeCount, stats.EdgeCount, stats.FileCount)
	return nil
}

// runUpdate incrementally updates the code intelligence graph.
func (c *CodegraphCommand) runUpdate() error {
	fmt.Fprintln(os.Stdout, console.GlyphInfo.Prefix()+"Updating code intelligence graph...")

	store, err := codegraph.NewStore("")
	if err != nil {
		return fmt.Errorf("failed to open codegraph store: %w", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := store.IndexChangedFiles(ctx, fileParser); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	stats := store.Stats()
	fmt.Fprintf(os.Stdout, "Code intelligence graph updated: %d nodes, %d edges, %d files\n",
		stats.NodeCount, stats.EdgeCount, stats.FileCount)
	return nil
}

// runStats shows node/edge/file counts from the code intelligence graph.
func (c *CodegraphCommand) runStats() error {
	store, err := openStore()
	if err != nil {
		return fmt.Errorf("failed to open codegraph store: %w", err)
	}
	if store == nil {
		fmt.Fprintln(os.Stdout, "The code intelligence graph has not been indexed yet. Run /codegraph build to index.")
		return nil
	}
	defer store.Close()

	stats := store.Stats()
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Code Intelligence Graph")
	fmt.Fprintln(os.Stdout, "══════════════════════════════")
	fmt.Fprintf(os.Stdout, "  Nodes: %d\n", stats.NodeCount)
	fmt.Fprintf(os.Stdout, "  Edges: %d\n", stats.EdgeCount)
	fmt.Fprintf(os.Stdout, "  Files: %d\n", stats.FileCount)
	fmt.Fprintln(os.Stdout)
	return nil
}

// Complete returns completions for the /codegraph command.
func (c *CodegraphCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	subcommands := []string{"build", "help", "stats", "update"}
	if len(args) == 0 {
		return subcommands
	}

	prefix := args[len(args)-1]
	if prefix == "" {
		return subcommands
	}

	var matches []string
	for _, sub := range subcommands {
		if strings.HasPrefix(strings.ToLower(sub), strings.ToLower(prefix)) {
			matches = append(matches, sub)
		}
	}
	return matches
}
