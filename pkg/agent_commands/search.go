package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/search"
)

// SearchCommand implements the /search slash command for searching across
// saved sessions by message content.
type SearchCommand struct{}

func (c *SearchCommand) Name() string {
	return "search"
}

func (c *SearchCommand) Description() string {
	return "Search across saved sessions by content"
}

func (c *SearchCommand) Usage() string {
	return strings.Join([]string{
		"Search across saved sessions by message content.",
		"",
		"Usage: /search <query> [flags]",
		"",
		"Flags:",
		"  --reindex      Force full index rebuild before searching",
		"  --cwd <dir>    Restrict to sessions in a specific working directory",
		"  --since <date> Only sessions with LastUpdated >= date (RFC3339 or YYYY-MM-DD)",
		"  --until <date> Only sessions with LastUpdated <= date",
		"  --limit <N>    Max results (default 20)",
		"  --json         Output as JSON array instead of formatted text",
		"",
		"Examples:",
		`  /search "embedding index"`,
		`  /search --reindex "auth error"`,
		`  /search --cwd /tmp --since 2026-01-01 "foo"`,
		`  /search --json "test"`,
	}, "\n")
}

// parseSearchFlags extracts flags from the raw args, returning the search
// options, a boolean for --reindex, and the remaining non-flag tokens
// joined as the query string. --json is already stripped by the registry
// before Execute is called so we never see it here.
func parseSearchFlags(args []string) (search.SearchOptions, bool, string, error) {
	var opts search.SearchOptions
	reindex := false
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
		case "--reindex":
			reindex = true
			i++
		case "--cwd":
			if i+1 >= len(args) {
				return opts, reindex, "", fmt.Errorf("--cwd requires a value")
			}
			i++
			opts.WorkingDir = args[i]
			i++
		case "--since":
			if i+1 >= len(args) {
				return opts, reindex, "", fmt.Errorf("--since requires a date value")
			}
			i++
			t, err := parseDate(args[i])
			if err != nil {
				return opts, reindex, "", fmt.Errorf("--since: %w", err)
			}
			opts.Since = t
			i++
		case "--until":
			if i+1 >= len(args) {
				return opts, reindex, "", fmt.Errorf("--until requires a date value")
			}
			i++
			t, err := parseDate(args[i])
			if err != nil {
				return opts, reindex, "", fmt.Errorf("--until: %w", err)
			}
			opts.Until = t
			i++
		case "--limit":
			if i+1 >= len(args) {
				return opts, reindex, "", fmt.Errorf("--limit requires a numeric value")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return opts, reindex, "", fmt.Errorf("--limit: invalid integer %q", args[i])
			}
			opts.Limit = n
			i++
		default:
			return opts, reindex, "", fmt.Errorf("unknown flag %q", arg)
		}
	}

	return opts, reindex, strings.Join(queryParts, " "), nil
}

// parseDate tries RFC3339 first, then falls back to YYYY-MM-DD.
func parseDate(date string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, date)
	if err == nil {
		return t, nil
	}
	t, err = time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (expected RFC3339 or YYYY-MM-DD)", date)
	}
	return t, nil
}

// getSessionsDir derives the sessions directory from the default index path.
// DefaultIndexPath returns ~/.sprout/sessions/search-index.json, so the
// sessions base directory is ~/.sprout/sessions/scoped/.
func getSessionsDir() string {
	idxPath := search.DefaultIndexPath()
	if idxPath == "" {
		return ""
	}
	parent := filepath.Dir(idxPath)
	return filepath.Join(parent, "scoped")
}

// runSearch loads/builds the index, executes the search, and returns results.
// Shared between Execute (text) and ExecuteWithJSONOutput (JSON).
func runSearch(args []string) ([]search.SearchResult, error) {
	opts, reindex, query, err := parseSearchFlags(args)
	if err != nil {
		return nil, err
	}

	if query == "" {
		return nil, fmt.Errorf("usage: /search <query> [--reindex] [--cwd <dir>] [--since <date>] [--until <date>] [--limit <N>] [--json]")
	}

	opts.Query = query

	path := search.DefaultIndexPath()
	idx, err := search.LoadIndex(path)
	if err != nil {
		return nil, fmt.Errorf("load search index: %w", err)
	}

	// Build if reindex requested or index is empty.
	if reindex || len(idx.Sessions) == 0 {
		sessionsDir := getSessionsDir()
		if sessionsDir == "" {
			return nil, fmt.Errorf("could not determine sessions directory")
		}
		idx, err = search.BuildIndex(sessionsDir, idx)
		if err != nil {
			return nil, fmt.Errorf("build search index: %w", err)
		}
		if err := search.SaveIndex(path, idx); err != nil {
			fmt.Fprintf(os.Stderr, "[search] warning: could not save index: %v\n", err)
		}
	}

	// (intentionally: do NOT error on empty index here — let Search() return
	// empty results for consistent behavior with the CLI)

	results := search.Search(idx, opts)
	if results == nil {
		results = []search.SearchResult{}
	}
	return results, nil
}

func (c *SearchCommand) Execute(args []string, chatAgent *agent.Agent) error {
	results, err := runSearch(args)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("No matching sessions.")
		return nil
	}

	plural := "s"
	if len(results) == 1 {
		plural = ""
	}
	fmt.Printf("%d session%s matched:\n\n", len(results), plural)
	fmt.Println(search.FormatResults(results))
	fmt.Println("\nUse '/sessions <#>' to load, or '/search --reindex' to rebuild the index.")
	return nil
}

func (c *SearchCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	results, err := runSearch(args)
	if err != nil {
		return err
	}

	return WriteJSONToOutput(results)
}
