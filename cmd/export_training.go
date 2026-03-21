// Export training data command for ledit
package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/training"
	"github.com/spf13/cobra"
)

var (
	exportFormat      string
	exportOutput      string
	exportAll         bool
	exportMinTurns    int
	exportMinActions  int
	exportNoToolRes   bool
	exportIncludeSys  bool
	exportSession     string
)

var exportTrainingCmd = &cobra.Command{
	Use:   "export-training",
	Short: "Export session data into training-ready formats",
	Long: `Convert ledit session data into formats suitable for LLM fine-tuning.

Supported formats:
  sharegpt  - ShareGPT JSON (conversations with metadata)
  openai    - OpenAI fine-tuning JSONL (one example per line)
  alpaca    - Alpaca instruction-following JSON

The command reads session files from ~/.ledit/sessions (or the directory
configured via LEDIT_CONFIG) and writes cleaned training data to the
specified output file.

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
  ledit export-training --format openai --output training.jsonl

  # Export everything across all directories as ShareGPT
  ledit export-training --format sharegpt --output data.json --all

  # Export a specific session as Alpaca format
  ledit export-training --format alpaca --output alpaca.json --session abc123

  # Include system prompts and keep raw tool results
  ledit export-training --format openai --output full.jsonl --include-system --no-tool-results=false`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()

		opts := training.ExportOptions{
			Format:        strings.ToLower(strings.TrimSpace(exportFormat)),
			Output:        exportOutput,
			All:           exportAll,
			MinTurns:      exportMinTurns,
			MinActions:    exportMinActions,
			NoToolResults: exportNoToolRes,
			IncludeSystem: exportIncludeSys,
			Session:       strings.TrimSpace(exportSession),
		}

		result, err := training.ExportSessions(opts)
		if err != nil {
			return fmt.Errorf("export failed: %w", err)
		}

		duration := time.Since(start)

		// Print summary to stdout.
		fmt.Fprintf(os.Stdout, "Export complete!\n")
		fmt.Fprintf(os.Stdout, "  Sessions scanned:   %d\n", result.SessionsScanned)
		fmt.Fprintf(os.Stdout, "  Sessions exported:  %d\n", result.SessionsExported)
		fmt.Fprintf(os.Stdout, "  Sessions filtered:  %d\n", result.SessionsFiltered)
		fmt.Fprintf(os.Stdout, "  Examples generated: %d\n", result.ExamplesGenerated)
		fmt.Fprintf(os.Stdout, "  Output:             %s\n", result.OutputPath)
		fmt.Fprintf(os.Stdout, "  Duration:           %s\n", duration.Round(time.Millisecond))

		return nil
	},
}

func init() {
	exportTrainingCmd.Flags().StringVar(&exportFormat, "format", "", "Output format: sharegpt, openai, or alpaca (required)")
	exportTrainingCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file path (required)")
	exportTrainingCmd.Flags().BoolVar(&exportAll, "all", false, "Export all sessions across all directories (not just current working directory)")
	exportTrainingCmd.Flags().IntVar(&exportMinTurns, "min-turns", 2, "Minimum number of user+assistant exchanges to include")
	exportTrainingCmd.Flags().IntVar(&exportMinActions, "min-actions", 1, "Minimum number of task actions to include")
	exportTrainingCmd.Flags().BoolVar(&exportNoToolRes, "no-tool-results", true, "Replace tool result messages with truncated placeholders (reduces training data size)")
	exportTrainingCmd.Flags().BoolVar(&exportIncludeSys, "include-system", false, "Include system prompts in the training data")
	exportTrainingCmd.Flags().StringVar(&exportSession, "session", "", "Export a specific session ID only")

	// Mark required flags for better error messages.
	_ = exportTrainingCmd.MarkFlagRequired("format")
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
}
