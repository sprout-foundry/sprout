//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/search"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search across saved sessions by content",
	Long: `Search across saved sessions by message content.
Builds or loads a search index then queries it.

Examples:
  sprout search "embedding index"
  sprout search --reindex "auth error"
  sprout search --json "test"
  sprout search --cwd /tmp --since 2026-01-01 "foo"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSearch(cmd, args)
	},
}

func init() {
	searchCmd.Flags().Bool("reindex", false, "Force full index rebuild before searching")
	searchCmd.Flags().String("cwd", "", "Restrict to sessions in a specific working directory")
	searchCmd.Flags().String("since", "", "Only sessions with LastUpdated >= date (RFC3339 or YYYY-MM-DD)")
	searchCmd.Flags().String("until", "", "Only sessions with LastUpdated <= date")
	searchCmd.Flags().Int("limit", 0, "Max results (default 20)")
	searchCmd.Flags().Bool("json", false, "Output as JSON array instead of formatted text")

	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	// Query is the positional args joined
	query := strings.Join(args, " ")

	// Read flags
	reindex, _ := cmd.Flags().GetBool("reindex")
	cwd, _ := cmd.Flags().GetString("cwd")
	sinceStr, _ := cmd.Flags().GetString("since")
	untilStr, _ := cmd.Flags().GetString("until")
	limit, _ := cmd.Flags().GetInt("limit")
	jsonOut, _ := cmd.Flags().GetBool("json")

	// Build search options
	opts := search.SearchOptions{
		Query:      query,
		WorkingDir: cwd,
		Limit:      limit,
	}

	// Parse date filters
	if sinceStr != "" {
		t, err := parseSearchDate(sinceStr)
		if err != nil {
			return fmt.Errorf("--since: %w", err)
		}
		opts.Since = t
	}
	if untilStr != "" {
		t, err := parseSearchDate(untilStr)
		if err != nil {
			return fmt.Errorf("--until: %w", err)
		}
		opts.Until = t
	}

	// Load existing index
	idx, err := search.LoadIndex(search.DefaultIndexPath())
	if err != nil {
		return fmt.Errorf("load search index: %w", err)
	}

	// Build if reindex requested or index is empty
	if reindex || len(idx.Sessions) == 0 {
		sessionsDir := getCLISessionsDir()
		if sessionsDir == "" {
			return fmt.Errorf("could not determine sessions directory")
		}
		idx, err = search.BuildIndex(sessionsDir, idx)
		if err != nil {
			return fmt.Errorf("build search index: %w", err)
		}
		if err := search.SaveIndex(search.DefaultIndexPath(), idx); err != nil {
			fmt.Fprintf(os.Stderr, "[search] warning: could not save index: %v\n", err)
		}
	}

	results := search.Search(idx, opts)

	if jsonOut {
		if results == nil {
			results = []search.SearchResult{}
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	formatted := search.FormatResults(results)
	fmt.Printf("%s\n", formatted)
	fmt.Printf("\n%d session%s matched\n", len(results), func() string {
		if len(results) == 1 {
			return ""
		}
		return "s"
	}())

	return nil
}

// parseSearchDate tries RFC3339 first, then falls back to YYYY-MM-DD.
func parseSearchDate(date string) (time.Time, error) {
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

// getCLISessionsDir derives the sessions directory for the CLI.
// DefaultIndexPath() returns ~/.sprout/sessions/search-index.json,
// so the sessions dir is ~/.sprout/sessions/scoped/.
func getCLISessionsDir() string {
	indexPath := search.DefaultIndexPath()
	if indexPath == "" {
		return ""
	}
	parent := filepath.Dir(indexPath)
	return filepath.Join(parent, "scoped")
}
