//go:build !js

// Export training data command for sprout
package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/training"
)

var (
	exportFormat           string
	exportOutput           string
	exportAll              bool
	exportMinTurns         int
	exportMinActions       int
	exportNoToolRes        bool
	exportIncludeSys       bool
	exportSession          string
	exportStructuredTools  bool
	exportIncludeSubagents bool
	exportSource           string
	exportMaxSize          int
	exportExclude          string
)

var exportTrainingCmd = &cobra.Command{
	Use:   "export-training",
	Short: "Export session data into training-ready formats",
	Long: `Convert sprout session data into formats suitable for LLM fine-tuning.

Data sources (--source):
  conversations  Session conversations (default)
  file-changes   File-change diff pairs from .sprout/changes directories
  all            Both conversations and file-changes written to the same output

Supported formats:
  sharegpt  - ShareGPT JSON (conversations with metadata)
  openai    - OpenAI fine-tuning JSONL (one example per line)
  alpaca    - Alpaca instruction-following JSON

File-change extraction (--source file-changes or all):
  Scans ALL .sprout/changes/*/metadata.json files across all projects.
  Each qualifying edit becomes an OpenAI JSONL example with the original
  file content (user) and the updated content (assistant).

Session filtering:
  --all            Export ALL sessions across all working directories
  --session <id>   Export a specific session only
  --min-turns N    Minimum user+assistant exchanges (default: 2)
  --min-actions N  Minimum task actions recorded (default: 1)

Message cleaning (when --no-tool-results is set, the default):
  • Tool-call messages from the assistant are converted to readable text
    describing the intended tool invocation.
  • Tool-result messages are replaced with short placeholders like
    "[tool result: success, 1234 chars]" to reduce size while preserving
    conversation flow.
  • System prompts are stripped (unless --include-system is set).
  • Consecutive messages with the same role are merged.

Examples:
  # Export all sessions in current directory as OpenAI JSONL
  sprout export-training --format openai --output training.jsonl

  # Export everything across all directories as ShareGPT
  sprout export-training --format sharegpt --output data.json --all

  # Export file-change diff pairs as training data
  sprout export-training --source file-changes --output edits.jsonl

  # Export conversations AND file-changes together
  sprout export-training --source all --output all.jsonl --all

  # Include subagent single-task examples alongside conversations
  sprout export-training --include-subagents --format openai --output all.jsonl

  # Include system prompts and keep raw tool results
  sprout export-training --format openai --output full.jsonl --include-system --no-tool-results=false`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()
		source := strings.ToLower(strings.TrimSpace(exportSource))
		excludePaths := parseExcludeList(exportExclude)

		switch source {
		case "", "conversations":
			return runConversationExport(start, excludePaths)
		case "file-changes":
			return runFileChangeExport(start, excludePaths)
		case "all":
			return runAllExport(start, excludePaths)
		default:
			return fmt.Errorf("unsupported --source %q: must be one of conversations, file-changes, all", source)
		}
	},
}

// runConversationExport runs the standard session conversation export.
func runConversationExport(start time.Time, excludePaths []string) error {
	opts := training.ExportOptions{
		Format:           strings.ToLower(strings.TrimSpace(exportFormat)),
		Output:           exportOutput,
		All:              exportAll,
		MinTurns:         exportMinTurns,
		MinActions:       exportMinActions,
		NoToolResults:    exportNoToolRes,
		IncludeSystem:    exportIncludeSys,
		Session:          strings.TrimSpace(exportSession),
		StructuredTools:  exportStructuredTools,
		IncludeSubagents: exportIncludeSubagents,
		ExcludePaths:     excludePaths,
	}

	result, err := training.ExportSessions(opts)
	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	printConversationSummary(result, time.Since(start))
	return nil
}

// runFileChangeExport runs the file-change diff-pair extractor.
func runFileChangeExport(start time.Time, excludePaths []string) error {
	opts := training.FileChangeExportOptions{
		Output:       exportOutput,
		MaxSize:      exportMaxSize,
		ExcludePaths: excludePaths,
	}

	result, err := training.ExportFileChanges(opts)
	if err != nil {
		return fmt.Errorf("file-change export failed: %w", err)
	}

	fmt.Fprintf(os.Stdout, "File-change export complete!\n")
	fmt.Fprintf(os.Stdout, "  Changes scanned:   %d\n", result.ChangesScanned)
	fmt.Fprintf(os.Stdout, "  Changes exported:  %d\n", result.ChangesExported)
	fmt.Fprintf(os.Stdout, "  Changes filtered:  %d\n", result.ChangesFiltered)
	fmt.Fprintf(os.Stdout, "  Output:            %s\n", result.OutputPath)
	fmt.Fprintf(os.Stdout, "  Duration:          %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// runAllExport runs both the conversation and file-change exports.
// Conversations are written to the --output path; file-changes are
// written to the same path with a ".file-changes" suffix before the
// extension so both files are produced from a single invocation.
func runAllExport(start time.Time, excludePaths []string) error {
	if err := runConversationExport(start, excludePaths); err != nil {
		return err
	}

	// Derive the file-changes output path from the conversation output.
	originalOutput := exportOutput
	fcOutput := deriveFileChangeOutputPath(originalOutput)
	exportOutput = fcOutput
	defer func() { exportOutput = originalOutput }()

	return runFileChangeExport(start, excludePaths)
}

// deriveFileChangeOutputPath inserts ".file-changes" before the file
// extension of the given path. If the path has no extension, the suffix
// is simply appended.
func deriveFileChangeOutputPath(path string) string {
	ext := ""
	dotIdx := strings.LastIndex(path, ".")
	slashIdx := strings.LastIndex(path, "/")
	if dotIdx > slashIdx {
		ext = path[dotIdx:]
		path = path[:dotIdx]
	}
	return path + ".file-changes" + ext
}

// printConversationSummary prints the standard conversation export stats.
func printConversationSummary(result *training.ExportResult, duration time.Duration) {
	fmt.Fprintf(os.Stdout, "Export complete!\n")
	fmt.Fprintf(os.Stdout, "  Sessions scanned:   %d\n", result.SessionsScanned)
	fmt.Fprintf(os.Stdout, "  Sessions exported:  %d\n", result.SessionsExported)
	fmt.Fprintf(os.Stdout, "  Sessions filtered:  %d\n", result.SessionsFiltered)
	fmt.Fprintf(os.Stdout, "  Examples generated: %d\n", result.ExamplesGenerated)
	fmt.Fprintf(os.Stdout, "  Output:             %s\n", result.OutputPath)
	fmt.Fprintf(os.Stdout, "  Duration:           %s\n", duration.Round(time.Millisecond))
}

func init() {
	exportTrainingCmd.Flags().StringVar(&exportFormat, "format", "", "Output format: sharegpt, openai, or alpaca (required for --source conversations)")
	exportTrainingCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file path (required)")
	exportTrainingCmd.Flags().BoolVar(&exportAll, "all", false, "Export all sessions across all directories (not just current working directory)")
	exportTrainingCmd.Flags().IntVar(&exportMinTurns, "min-turns", 2, "Minimum number of user+assistant exchanges to include")
	exportTrainingCmd.Flags().IntVar(&exportMinActions, "min-actions", 1, "Minimum number of task actions to include")
	exportTrainingCmd.Flags().BoolVar(&exportNoToolRes, "no-tool-results", true, "Replace tool result messages with truncated placeholders (reduces training data size)")
	exportTrainingCmd.Flags().BoolVar(&exportIncludeSys, "include-system", false, "Include system prompts in the training data")
	exportTrainingCmd.Flags().StringVar(&exportSession, "session", "", "Export a specific session ID only")
	exportTrainingCmd.Flags().BoolVar(&exportStructuredTools, "structured-tools", false, "Preserve structured tool-call format (OpenAI function-calling schema)")
	exportTrainingCmd.Flags().BoolVar(&exportIncludeSubagents, "include-subagents", false, "Extract single-task examples from run_subagent/run_parallel_subagents tool calls (openai format only)")
	exportTrainingCmd.Flags().StringVar(&exportSource, "source", "conversations", "Data source: conversations, file-changes, or all")
	exportTrainingCmd.Flags().IntVar(&exportMaxSize, "max-size", 0, "Max chars per file content for file-change export (default: 50000)")
	exportTrainingCmd.Flags().StringVar(&exportExclude, "exclude", "", "Comma-separated list of directory prefixes to exclude (e.g. /Users/alice/dev/client,/Users/alice/dev/private)")

	// Mark required flags for better error messages.
	_ = exportTrainingCmd.MarkFlagRequired("output")

	// Register flag completions.
	_ = exportTrainingCmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		formats := []string{"sharegpt", "openai", "alpaca"}
		var matches []string
		for _, f := range formats {
			if strings.HasPrefix(f, toComplete) {
				matches = append(matches, f)
			}
		}
		return matches, cobra.ShellCompDirectiveNoFileComp
	})
	_ = exportTrainingCmd.RegisterFlagCompletionFunc("source", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		sources := []string{"conversations", "file-changes", "all"}
		var matches []string
		for _, s := range sources {
			if strings.HasPrefix(s, toComplete) {
				matches = append(matches, s)
			}
		}
		return matches, cobra.ShellCompDirectiveNoFileComp
	})
}

// parseExcludeList splits a comma-separated list of directory prefixes
// into a normalized slice for path matching.
func parseExcludeList(raw string) []string {
	var result []string
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}
