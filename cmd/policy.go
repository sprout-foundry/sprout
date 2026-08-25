//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"gopkg.in/yaml.v3"
)

// policyCmd is the top-level `sprout policy` command.
var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage shell permission policy",
	Long: `Manage the shell permission policy: list, add, remove, import, export,
and trust workspace overlay configurations.

The policy system controls which shell commands require agent approval.
Patterns define safe (auto-approved) and dangerous (gated) command prefixes.`,
}

// ---------------------------------------------------------------------------
// `policy list` — Human-readable effective policy
// ---------------------------------------------------------------------------

var policyListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show the effective shell policy",
	Long:  `Display a summary of the current shell policy configuration, including user-defined patterns and the workspace overlay mode.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("=== Shell Permission Policy ===")
		fmt.Println()

		// User safe patterns
		safe := config.Shell.UserSafePatterns
		fmt.Printf("User Safe Patterns (%d):\n", len(safe))
		if len(safe) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, p := range safe {
				fmt.Printf("  - %s (%s)\n", p.Match, p.Kind)
			}
		}
		fmt.Println()

		// User dangerous patterns
		danger := config.Shell.UserDangerousPatterns
		fmt.Printf("User Dangerous Patterns (%d):\n", len(danger))
		if len(danger) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, p := range danger {
				fmt.Printf("  - %s (%s)\n", p.Match, p.Kind)
			}
		}
		fmt.Println()

		// Workspace overlay mode
		mode := config.Shell.WorkspaceOverlay.Mode
		if mode == "" {
			mode = "tighten_only"
		}
		fmt.Printf("Workspace Overlay Mode: %s\n", mode)
		return nil
	},
}

// ---------------------------------------------------------------------------
// `policy dump` — Full effective policy with source annotations
// ---------------------------------------------------------------------------

type policyDumpOutput struct {
	UserSafePatterns      []policyPatternSource `json:"user_safe_patterns" yaml:"user_safe_patterns"`
	UserDangerousPatterns []policyPatternSource `json:"user_dangerous_patterns" yaml:"user_dangerous_patterns"`
	WorkspaceOverlay      policyOverlaySource   `json:"workspace_overlay" yaml:"workspace_overlay"`
}

type policyPatternSource struct {
	Match  string `json:"match" yaml:"match"`
	Kind   string `json:"kind" yaml:"kind"`
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Source string `json:"source" yaml:"source"`
}

type policyOverlaySource struct {
	Mode string `json:"mode" yaml:"mode"`
}

var policyDumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump the full effective policy as JSON or YAML",
	Long: `Output the entire effective shell policy with source annotations.
Each pattern is tagged with its source (user-config). Default format is JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		if format == "" {
			format = "json"
		}

		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		out := policyDumpOutput{}

		for _, p := range config.Shell.UserSafePatterns {
			out.UserSafePatterns = append(out.UserSafePatterns, policyPatternSource{
				Match:  p.Match,
				Kind:   p.Kind,
				Reason: p.Reason,
				Source: "user-config",
			})
		}
		for _, p := range config.Shell.UserDangerousPatterns {
			out.UserDangerousPatterns = append(out.UserDangerousPatterns, policyPatternSource{
				Match:  p.Match,
				Kind:   p.Kind,
				Reason: p.Reason,
				Source: "user-config",
			})
		}

		mode := config.Shell.WorkspaceOverlay.Mode
		if mode == "" {
			mode = "tighten_only"
		}
		out.WorkspaceOverlay = policyOverlaySource{Mode: mode}

		return formatOutput(out, format)
	},
}

// ---------------------------------------------------------------------------
// `policy add safe|dangerous <pattern>`
// ---------------------------------------------------------------------------

var policyAddCmd = &cobra.Command{
	Use:   "add safe|dangerous <pattern>",
	Short: "Add a user-defined shell policy pattern",
	Long: `Add a new shell pattern to the user configuration.

Examples:
  sprout policy add safe 'my-tool'
  sprout policy add dangerous 'terraform destroy'

The pattern is added as a prefix match by default.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tier := strings.ToLower(args[0])
		pattern := args[1]

		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("pattern must not be empty")
		}

		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Check for duplicates before adding
		switch tier {
		case "safe":
			if findPattern(config.Shell.UserSafePatterns, pattern) >= 0 {
				return fmt.Errorf("pattern '%s' already exists in safe patterns", pattern)
			}
		case "dangerous":
			if findPattern(config.Shell.UserDangerousPatterns, pattern) >= 0 {
				return fmt.Errorf("pattern '%s' already exists in dangerous patterns", pattern)
			}
		default:
			return fmt.Errorf("invalid tier %q (must be 'safe' or 'dangerous')", tier)
		}

		sp := configuration.ShellPattern{
			Match: pattern,
			Kind:  "prefix",
		}

		switch tier {
		case "safe":
			config.Shell.UserSafePatterns = append(config.Shell.UserSafePatterns, sp)
		case "dangerous":
			config.Shell.UserDangerousPatterns = append(config.Shell.UserDangerousPatterns, sp)
		}

		if err := config.Save(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("Added %s pattern: %s\n", tier, pattern)
		return nil
	},
}

// ---------------------------------------------------------------------------
// `policy remove safe|dangerous <pattern>`
// ---------------------------------------------------------------------------

var policyRemoveCmd = &cobra.Command{
	Use:   "remove safe|dangerous <pattern>",
	Short: "Remove a user-defined shell policy pattern",
	Long: `Remove the first matching pattern from the user configuration.

Examples:
  sprout policy remove safe 'my-tool'
  sprout policy remove dangerous 'terraform destroy'`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tier := strings.ToLower(args[0])
		pattern := args[1]

		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		switch tier {
		case "safe":
			idx := findPattern(config.Shell.UserSafePatterns, pattern)
			if idx < 0 {
				return fmt.Errorf("pattern '%s' not found in %s patterns", pattern, tier)
			}
			config.Shell.UserSafePatterns = removePattern(config.Shell.UserSafePatterns, idx)
		case "dangerous":
			idx := findPattern(config.Shell.UserDangerousPatterns, pattern)
			if idx < 0 {
				return fmt.Errorf("pattern '%s' not found in %s patterns", pattern, tier)
			}
			config.Shell.UserDangerousPatterns = removePattern(config.Shell.UserDangerousPatterns, idx)
		default:
			return fmt.Errorf("invalid tier %q (must be 'safe' or 'dangerous')", tier)
		}

		if err := config.Save(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("Removed %s pattern: %s\n", tier, pattern)
		return nil
	},
}

func findPattern(patterns []configuration.ShellPattern, match string) int {
	for i, p := range patterns {
		if p.Match == match {
			return i
		}
	}
	return -1
}

func removePattern(patterns []configuration.ShellPattern, idx int) []configuration.ShellPattern {
	return append(patterns[:idx], patterns[idx+1:]...)
}

// ---------------------------------------------------------------------------
// `policy export [--format=yaml|json]`
// ---------------------------------------------------------------------------

type policyExportData struct {
	UserSafePatterns      []configuration.ShellPattern `json:"user_safe_patterns" yaml:"user_safe_patterns"`
	UserDangerousPatterns []configuration.ShellPattern `json:"user_dangerous_patterns" yaml:"user_dangerous_patterns"`
}

var policyExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export user-defined patterns to a file-compatible format",
	Long: `Export only the user-defined shell patterns (not built-ins) in YAML or JSON.
The output can be used with ` + "`sprout policy import`" + ` to restore on another machine.

Default format is YAML.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		if format == "" {
			format = "yaml"
		}

		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		data := policyExportData{
			UserSafePatterns:      config.Shell.UserSafePatterns,
			UserDangerousPatterns: config.Shell.UserDangerousPatterns,
		}

		return formatOutput(data, format)
	},
}

// ---------------------------------------------------------------------------
// `policy import <file>`
// ---------------------------------------------------------------------------

var policyImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import and merge patterns from a file",
	Long: `Import shell policy patterns from a YAML or JSON file. Patterns are appended
to the existing user configuration (merge, not replace).

File format is detected from the extension (.yaml/.yml = YAML, .json = JSON).

Example:
  sprout policy import policy.yaml`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		// Detect format from extension
		ext := strings.ToLower(filepath.Ext(filePath))
		var format string
		switch ext {
		case ".yaml", ".yml":
			format = "yaml"
		case ".json":
			format = "json"
		default:
			return fmt.Errorf("unsupported file format %q (use .yaml, .yml, or .json)", ext)
		}

		// Read file
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}

		// Parse into import struct
		var imported policyExportData
		switch format {
		case "yaml":
			if err := yaml.Unmarshal(data, &imported); err != nil {
				return fmt.Errorf("failed to parse YAML: %w", err)
			}
		case "json":
			if err := json.Unmarshal(data, &imported); err != nil {
				return fmt.Errorf("failed to parse JSON: %w", err)
			}
		}

		// Load current config
		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Merge with deduplication: only add patterns that don't already exist
		newSafe := 0
		dupSafe := 0
		for _, p := range imported.UserSafePatterns {
			if findPattern(config.Shell.UserSafePatterns, p.Match) < 0 {
				config.Shell.UserSafePatterns = append(config.Shell.UserSafePatterns, p)
				newSafe++
			} else {
				dupSafe++
			}
		}
		newDanger := 0
		dupDanger := 0
		for _, p := range imported.UserDangerousPatterns {
			if findPattern(config.Shell.UserDangerousPatterns, p.Match) < 0 {
				config.Shell.UserDangerousPatterns = append(config.Shell.UserDangerousPatterns, p)
				newDanger++
			} else {
				dupDanger++
			}
		}

		if err := config.Save(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		if dupSafe > 0 || dupDanger > 0 {
			fmt.Printf("Imported %d new safe patterns (%d duplicates skipped), %d new dangerous patterns (%d duplicates skipped)\n",
				newSafe, dupSafe, newDanger, dupDanger)
		} else {
			fmt.Printf("Imported %d safe patterns, %d dangerous patterns\n",
				newSafe, newDanger)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// formatOutput marshals data to JSON or YAML and writes to stdout.
func formatOutput(data interface{}, format string) error {
	var content []byte
	var err error

	switch strings.ToLower(format) {
	case "json":
		content, err = json.MarshalIndent(data, "", "  ")
	case "yaml":
		content, err = yaml.Marshal(data)
	default:
		return fmt.Errorf("unsupported format %q (use json or yaml)", format)
	}
	if err != nil {
		return fmt.Errorf("failed to format output: %w", err)
	}

	fmt.Println(string(content))
	return nil
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func init() {
	rootCmd.AddCommand(policyCmd)

	policyCmd.AddCommand(policyListCmd)
	policyCmd.AddCommand(policyDumpCmd)
	policyCmd.AddCommand(policyAddCmd)
	policyCmd.AddCommand(policyRemoveCmd)
	policyCmd.AddCommand(policyExportCmd)
	policyCmd.AddCommand(policyImportCmd)

	// Flags
	policyDumpCmd.Flags().String("format", "json", "Output format: json or yaml")
	policyExportCmd.Flags().String("format", "yaml", "Output format: json or yaml")
}
