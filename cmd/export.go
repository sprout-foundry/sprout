//go:build !js

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/export"
)

// ---------------------------------------------------------------------------
// Command definition
// ---------------------------------------------------------------------------

var exportCmd = &cobra.Command{
	Use:   "export [session-id]",
	Short: "Export a saved session to markdown, HTML, or JSON",
	Long: `Export a saved session to a file or stdout.

By default exports to markdown on stdout.  Use --format to switch to html or
json and --output to write to a file instead of stdout.

When --all is used every saved session is exported one after another.
For json a top-level JSON array is produced; for markdown and html sessions
are separated by horizontal rules.

Examples:
  sprout export my-session-id
  sprout export my-session-id --format json --output session.json
  sprout export --latest --format html
  sprout export --all --output all-sessions.md`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runExport(cmd, args)
	},
}

func init() {
	exportCmd.Flags().String("format", "markdown", "Output format: markdown, html, or json")
	exportCmd.Flags().String("output", "", "Write to file instead of stdout (default: stdout)")
	exportCmd.Flags().Bool("latest", false, "Export the most-recently-updated session")
	exportCmd.Flags().Bool("all", false, "Export all saved sessions (concatenated)")
	exportCmd.Flags().Bool("include-tool-calls", false, "Include tool call details in the output")
	exportCmd.Flags().Bool("no-cost", false, "Omit cost/tokens in the output")
	exportCmd.Flags().Bool("no-secret-redaction", false, "Disable secret redaction in exported content")
	exportCmd.Flags().Bool("no-pretty-json", false, "Do not pretty-print JSON output (json format only)")

	rootCmd.AddCommand(exportCmd)
}

// ---------------------------------------------------------------------------
// Execution
// ---------------------------------------------------------------------------

func runExport(cmd *cobra.Command, args []string) error {
	// Read flags
	formatStr, _ := cmd.Flags().GetString("format")
	outputPath, _ := cmd.Flags().GetString("output")
	latest, _ := cmd.Flags().GetBool("latest")
	all, _ := cmd.Flags().GetBool("all")
	incTool, _ := cmd.Flags().GetBool("include-tool-calls")
	noCost, _ := cmd.Flags().GetBool("no-cost")
	noRedact, _ := cmd.Flags().GetBool("no-secret-redaction")
	noPretty, _ := cmd.Flags().GetBool("no-pretty-json")

	// Validate format
	format := export.ExportFormat(strings.ToLower(formatStr))
	switch format {
	case export.FormatMarkdown, export.FormatHTML, export.FormatJSON:
	default:
		return fmt.Errorf("invalid format %q — must be markdown, html, or json", formatStr)
	}

	// Validate mutual exclusions
	if len(args) > 0 && (latest || all) {
		return fmt.Errorf("provide either a session-id or --latest/--all, not both")
	}
	if latest && all {
		return fmt.Errorf("--latest and --all are mutually exclusive")
	}

	// Build export options
	opts := export.ExportOptions{
		IncludeToolCalls: incTool,
		IncludeCost:      !noCost,
		RedactSecrets:    !noRedact,
		PrettyPrintJSON:  !noPretty,
	}

	// Determine output writer
	var writer io.Writer = os.Stdout
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		writer = f
	}

	// Route to the right path
	switch {
	case all:
		return exportAllSessions(writer, format, opts)
	case latest:
		sid, err := findLatestSessionID()
		if err != nil {
			return err
		}
		return exportOneSession(writer, sid, format, opts)
	case len(args) == 1:
		return exportOneSession(writer, args[0], format, opts)
	default:
		return fmt.Errorf("provide a session-id or use --latest or --all")
	}
}

// ---------------------------------------------------------------------------
// Single session export
// ---------------------------------------------------------------------------

func exportOneSession(w io.Writer, sessionID string, format export.ExportFormat, opts export.ExportOptions) error {
	cs, err := agent.LoadStateWithoutAgent(sessionID)
	if err != nil {
		return fmt.Errorf("load session %q: %w", sessionID, err)
	}

	src := conversationStateToSource(cs)
	if err := export.Export(w, src, format, opts); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// All sessions export
// ---------------------------------------------------------------------------

func exportAllSessions(w io.Writer, format export.ExportFormat, opts export.ExportOptions) error {
	sessions, err := agent.ListAllSessionsWithTimestamps()
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(w, "No sessions found.")
		return nil
	}

	// Sort newest-first for --all output
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
	})

	if format == export.FormatJSON {
		// For JSON we emit a top-level array so the result is valid JSON
		// even with multiple sessions.
		var sources []export.SessionSource
		for _, si := range sessions {
			cs, err := loadSessionByPath(si.StoragePath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", si.SessionID, err)
				continue
			}
			sources = append(sources, conversationStateToSource(cs))
		}
		if len(sources) == 0 {
			fmt.Fprintln(w, "[]")
			return nil
		}
		var data []byte
		if opts.PrettyPrintJSON {
			data, _ = json.MarshalIndent(sources, "", "  ")
		} else {
			data, _ = json.Marshal(sources)
		}
		data = append(data, '\n')
		_, err = w.Write(data)
		return err
	}

	// For markdown / html, concatenate with separators
	for i, si := range sessions {
		if i > 0 {
			if format == export.FormatMarkdown {
				fmt.Fprintln(w, "---")
			}
		}

		cs, err := loadSessionByPath(si.StoragePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", si.SessionID, err)
			continue
		}
		src := conversationStateToSource(cs)
		if err := export.Export(w, src, format, opts); err != nil {
			return fmt.Errorf("export %s: %w", si.SessionID, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func findLatestSessionID() (string, error) {
	sessions, err := agent.ListAllSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions found")
	}
	// Sort globally newest-first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
	})
	return sessions[0].SessionID, nil
}

// loadSessionByPath reads a ConversationState from an absolute file path.
func loadSessionByPath(path string) (*agent.ConversationState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cs agent.ConversationState
	if err := json.Unmarshal(data, &cs); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return &cs, nil
}

// conversationStateToSource maps an internal ConversationState to
// export.SessionSource.  The mapping is intentionally neutral — the
// export package owns the output format; this adapter just bridges the
// two representations.
func conversationStateToSource(cs *agent.ConversationState) export.SessionSource {
	now := time.Now()
	startedAt := cs.LastUpdated
	if startedAt.IsZero() {
		startedAt = now
	}

	src := export.SessionSource{
		ID:               cs.SessionID,
		Name:             cs.Name,
		WorkingDirectory: cs.WorkingDirectory,
		StartedAt:        startedAt,
		LastUpdated:      cs.LastUpdated,
		TotalCost:        cs.TotalCost,
		InputTokens:      cs.PromptTokens,
		OutputTokens:     cs.CompletionTokens,
	}
	// Build messages
	for _, m := range cs.Messages {
		src.Messages = append(src.Messages, messageToSource(m))
	}
	return src
}

func messageToSource(m api.Message) export.MessageSource {
	src := export.MessageSource{
		Role:    m.Role,
		Content: m.Content,
	}
	// Tool calls from the API message
	for _, tc := range m.ToolCalls {
		src.ToolCalls = append(src.ToolCalls, export.ToolCallSource{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	// Reasoning content — append it to the content as a distinct block
	if m.ReasoningContent != "" {
		src.Content += "\n\n<reasoning>\n" + m.ReasoningContent + "\n</reasoning>"
	}
	return src
}

// ---------------------------------------------------------------------------
// Test helpers — exported so export_test.go can use them
// ---------------------------------------------------------------------------

// WriteTestSession creates a valid session JSON file in the scoped sessions
// directory for a given session ID and working directory.  Returns the
// absolute path of the written file.
func WriteTestSession(stateDir, sessionID, workingDir string, cs agent.ConversationState) (string, error) {
	scopeHash := workingDirScopeHash(workingDir)
	scopeDir := filepath.Join(stateDir, "scoped", scopeHash)
	if err := os.MkdirAll(scopeDir, 0o700); err != nil {
		return "", fmt.Errorf("create scope dir: %w", err)
	}
	path := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionID))
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write session file: %w", err)
	}
	return path, nil
}

// workingDirScopeHash mirrors agent.workingDirectoryScopeHash so tests
// can place files in the right scoped directory without importing agent
// internals.
func workingDirScopeHash(workingDir string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(workingDir))))
	return hex.EncodeToString(sum[:8])
}
