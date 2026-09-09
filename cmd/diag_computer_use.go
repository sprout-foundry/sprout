//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/agent_tools/computer_use"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

var diagCUJSON bool

var diagComputerUseCmd = &cobra.Command{
	Use:   "computer-use",
	Short: "Preflight computer use: tools, permissions, config gates",
	Long: `Check everything the computer_user persona needs before starting a session:

  1. Platform tools   — screencapture/cliclick (macOS) or scrot/xdotool (Linux)
  2. OS permissions   — Screen Recording + Accessibility on macOS
  3. Config gates     — computer_use.enabled and related settings

Exits non-zero when any required piece is missing, so it can gate scripts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDiagComputerUse()
	},
}

func runDiagComputerUse() error {
	support := computer_use.CheckPlatformSupport()
	perms := computer_use.CheckPermissions()

	cfg, cfgErr := configuration.Load()
	cuEnabled := false
	perMinute := 0
	panicChord := ""
	if cfgErr == nil && cfg != nil && cfg.ComputerUse != nil {
		resolved := cfg.ComputerUse.Resolve()
		cuEnabled = resolved.Enabled
		perMinute = resolved.MaxActionsPerMinute
		panicChord = resolved.PanicKeyChord
	}

	type check struct {
		Label string
		OK    bool
		Note  string
	}
	checks := []check{
		{"Platform (" + support.OS + ")", support.Supported, support.Reason},
	}
	for _, p := range perms {
		checks = append(checks, check{Label: p.Name, OK: p.OK, Note: p.Detail})
		if !p.OK && p.FixHint != "" {
			checks = append(checks, check{Label: "  fix", OK: false, Note: p.FixHint})
		}
	}
	checks = append(checks, check{"computer_use.enabled", cuEnabled,
		map[bool]string{true: "enabled", false: "disabled — set computer_use.enabled = true in settings"}[cuEnabled]})

	if diagCUJSON {
		out := map[string]any{
			"supported":           support.Supported,
			"os":                  support.OS,
			"reason":              support.Reason,
			"permissions":         perms,
			"config_enabled":      cuEnabled,
			"max_actions_per_min": perMinute,
			"panic_key_chord":     panicChord,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	fmt.Println("=== Computer Use Preflight ===")
	fmt.Println()
	allOK := true
	for _, c := range checks {
		glyph := console.GlyphSuccess.Prefix()
		if !c.OK {
			glyph = console.GlyphError.Prefix()
			allOK = false
			if c.Label == "  fix" {
				glyph = "    → "
			}
		}
		note := ""
		if c.Note != "" {
			note = " — " + c.Note
		}
		fmt.Printf("  %s %s%s\n", glyph, c.Label, note)
	}

	if allOK {
		fmt.Println()
		console.GlyphSuccess.Fprintln(os.Stdout, "Ready: activate with the computer_user persona in an interactive agent session.")
		fmt.Printf("  rate limit: %d actions/min · panic chord: %s\n", perMinute, panicChord)
		return nil
	}

	fmt.Println()
	console.GlyphWarning.Fprintln(os.Stdout, "Not ready — fix the failures above, then re-run `sprout diag computer-use`.")
	return fmt.Errorf("computer use preflight failed")
}

func init() {
	diagComputerUseCmd.Flags().BoolVar(&diagCUJSON, "json", false, "Output machine-readable JSON")
	diagCmd.AddCommand(diagComputerUseCmd)
}
