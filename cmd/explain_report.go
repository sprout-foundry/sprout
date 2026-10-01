//go:build !js

package cmd

// explain_report.go — the /explain reporting layer: risk-level
// assessment (riskLevelFromSecurityResult, combinedAssessment,
// levelHeadline, suppressionHints) and the human/JSON output
// formats. Split out of explain.go.
import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/shelltext"
)

// riskLevelFromSecurityResult converts a tools.SecurityResult risk tier into
// a configuration.RiskLevel. Replaces the deleted tools.RiskLevelFromSecurityResult.
// Mapping: hard-block → critical, safe → low, caution → medium, dangerous → high.
// Unknown risk values default to low (matching the prior behavior).
func riskLevelFromSecurityResult(res tools.SecurityResult) configuration.RiskLevel {
	if res.IsHardBlock {
		return configuration.RiskLevelCritical
	}
	switch res.Risk {
	case tools.SecuritySafe:
		return configuration.RiskLevelLow
	case tools.SecurityCaution:
		return configuration.RiskLevelMedium
	case tools.SecurityDangerous:
		return configuration.RiskLevelHigh
	default:
		return configuration.RiskLevelLow
	}
}

// combinedAssessment folds the git-history-rewrite gate into the classifier
// verdict to produce the final level and hard-block status.
func combinedAssessment(toolName string, secResult tools.SecurityResult, args map[string]interface{}) (configuration.RiskLevel, bool) {
	level := riskLevelFromSecurityResult(secResult)
	hardBlock := secResult.IsHardBlock

	if toolName == "shell_command" {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			if shelltext.IsGitHistoryRewriteCommand(cmd) {
				if isGitRebaseCommand(cmd) {
					// AGENTS.md: rebase is unconditionally banned — hard-block.
					level = configuration.RiskLevelCritical
					hardBlock = true
				} else {
					// Other history-rewrite ops: branch -D, tag -d, reset --hard <commit-ish>
					level = configuration.RiskLevelHigh
				}
			}
		}
	}
	return level, hardBlock
}

// levelHeadline renders the top-line summary of the assessment.
func levelHeadline(level configuration.RiskLevel, res tools.SecurityResult) string {
	intent := res.IntentConfirmation
	switch level {
	case configuration.RiskLevelCritical:
		return "CRITICAL — hard-block (cannot be approved)"
	case configuration.RiskLevelHigh:
		if intent {
			return "HIGH — requires explicit confirmation before proceeding"
		}
		return "HIGH — prompts when interactive; blocked when non-interactive"
	case configuration.RiskLevelMedium:
		if intent {
			return "MEDIUM — requires explicit confirmation before proceeding"
		}
		return "MEDIUM — prompts when interactive; auto-approved risk-profile dependent"
	default:
		if intent {
			return "LOW — requires explicit confirmation before proceeding"
		}
		return "LOW — auto-approved (no prompt)"
	}
}

// suppressionHints returns guidance on how to bypass a non-critical prompt.
// Only shown for Medium and High — Low commands are auto-approved and
// Critical cannot be overridden.
func suppressionHints(level configuration.RiskLevel, toolName string) []string {
	if level == configuration.RiskLevelCritical || level == configuration.RiskLevelLow {
		return nil
	}
	var hints []string
	if toolName == "shell_command" {
		hints = append(hints, "--unsafe-shell          (shell commands only)")
	}
	hints = append(hints, "--risk-profile=permissive  (all non-critical operations)")
	hints = append(hints, "Re-run interactively to approve via the webui/CLI dialog")
	return hints
}

// printExplainHuman writes the human-readable assessment to stdout.
func printExplainHuman(cmd *cobra.Command, res tools.SecurityResult, level configuration.RiskLevel, sources []explainSource, toolName string, args map[string]interface{}) {
	out := cmd.OutOrStdout()

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", levelHeadline(level, res))
	fmt.Fprintln(out)

	if c, _ := args["command"].(string); c != "" {
		fmt.Fprintf(out, "  Command:  %s\n", c)
	}
	if p, _ := args["path"].(string); p != "" {
		fmt.Fprintf(out, "  Path:     %s\n", p)
	}
	if op, _ := args["operation"].(string); op != "" {
		fmt.Fprintf(out, "  Operation: %s\n", op)
	}
	fmt.Fprintf(out, "  Tool:     %s\n", toolName)
	fmt.Fprintln(out)

	reason := strings.TrimSpace(res.Reasoning)
	if reason == "" {
		reason = "(no reasoning provided)"
	}
	fmt.Fprintf(out, "  Reason: %s\n", reason)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Contributing checks:")
	for _, s := range sources {
		fmt.Fprintf(out, "    \u2022 %-19s \u2014 %s\n", s.id, s.explain)
	}

	if level == configuration.RiskLevelCritical {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  This operation will be unconditionally blocked. No risk profile,")
		fmt.Fprintln(out, "  flag, or approval can override it.")
	}

	if hints := suppressionHints(level, toolName); len(hints) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  To suppress this prompt:")
		for _, h := range hints {
			fmt.Fprintf(out, "    \u2022 %s\n", h)
		}
	}
	fmt.Fprintln(out)
}

// explainJSONOutput is the structure emitted with --json.
type explainJSONOutput struct {
	Tool      string                 `json:"tool"`
	Args      map[string]interface{} `json:"args"`
	RiskLevel string                 `json:"risk_level"`
	HardBlock bool                   `json:"hard_block"`
	Sources   []explainJSONSource    `json:"sources"`
	Result    tools.SecurityResult   `json:"result"`
}

// explainJSONSource is a single contributing check in JSON output.
type explainJSONSource struct {
	ID      string `json:"id"`
	Explain string `json:"explain"`
	Level   string `json:"level"`
}

// printExplainJSON writes a machine-readable assessment to stdout.
func printExplainJSON(cmd *cobra.Command, res tools.SecurityResult, level configuration.RiskLevel, sources []explainSource, toolName string, args map[string]interface{}) error {
	jsonSources := make([]explainJSONSource, 0, len(sources))
	for _, s := range sources {
		jsonSources = append(jsonSources, explainJSONSource{
			ID:      s.id,
			Explain: s.explain,
			Level:   string(s.level),
		})
	}
	payload := explainJSONOutput{
		Tool:      toolName,
		Args:      args,
		RiskLevel: string(level),
		HardBlock: res.IsHardBlock,
		Sources:   jsonSources,
		Result:    res,
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// explainToolList returns a sorted, comma-separated list of supported tools.
func explainToolList() string {
	names := make([]string, 0, len(explainSupportedTools))
	for name := range explainSupportedTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
