//go:build !js

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// agentHelpAll, when set via --help-all, makes `sprout agent --help` (and a
// bare `sprout agent --help-all`) list every flag instead of just the common
// ones.
var agentHelpAll bool

// advancedAgentFlags are the rarely-used / power-user flags hidden from the
// default `sprout agent --help`. The everyday flags (provider, model,
// no-web-ui, daemon, session, persona, dry-run, no-stream, skip-prompt) stay
// visible; these are surfaced on demand with --help-all. Hiding happens at
// help-render time (agentHelpFunc) so it doesn't depend on cross-file init
// ordering and never affects flag *parsing* — every flag still works.
var advancedAgentFlags = []string{
	"no-connection-check",
	"risk-profile",
	"ea-mode",
	"max-iterations",
	"show-reasoning-terminal",
	"system-prompt",
	"system-prompt-str",
	"unsafe",
	"unsafe-shell",
	"no-subagents",
	"subagent-model",
	"subagent-provider",
	"resource-directory",
	"workflow-config",
	"budget-usd",
	"budget-warn",
	"heartbeat",
	"trace-dataset-dir",
	"prompt-stdin",
	"no-project-skills",
	"web-port",
	"bind",
	"bind-socket",
	"secret",
	"output-json",
}

func init() {
	agentCmd.Flags().BoolVar(&agentHelpAll, "help-all", false, "Show all flags, including advanced/rarely-used ones")

	// Render common flags by default; reveal everything with --help-all. Capture
	// the inherited (default) help func and wrap it so we toggle flag visibility
	// around the standard rendering.
	defaultHelp := agentCmd.HelpFunc()
	agentCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		setAdvancedAgentFlagsHidden(cmd, !agentHelpAll)
		defaultHelp(cmd, args)
		if !agentHelpAll {
			fmt.Fprintf(cmd.OutOrStdout(),
				"\n%d advanced flags hidden. Run 'sprout agent --help-all' to see them.\n",
				len(advancedAgentFlags))
		}
		// Restore visibility so the toggle never leaks into other code paths.
		setAdvancedAgentFlagsHidden(cmd, false)
	})
}

// setAdvancedAgentFlagsHidden marks (or unmarks) the advanced flags as hidden.
// Hidden only affects help output; the flags remain fully parseable.
func setAdvancedAgentFlagsHidden(cmd *cobra.Command, hidden bool) {
	for _, name := range advancedAgentFlags {
		if f := cmd.Flags().Lookup(name); f != nil {
			f.Hidden = hidden
		}
	}
}

// maybeRenderAgentHelpAll handles a bare `sprout agent --help-all` (no -h):
// cobra only invokes the help func for -h/--help, so when --help-all is passed
// on its own we render help here and signal the caller to stop. Returns true
// when help was shown and the command should not run.
func maybeRenderAgentHelpAll(cmd *cobra.Command) bool {
	if !agentHelpAll {
		return false
	}
	_ = cmd.Help()
	return true
}
