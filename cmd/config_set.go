//go:build !js

package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a configuration value by key. Writes the layer the agent reads:
the workspace config inside a git repository, the global config elsewhere
(or always, with --global).

Examples:
  sprout config set output_verbosity compact
  sprout config set risk_profile cautious

Run 'sprout config set --help-keys' to list the settable keys.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if configSetListKeys {
			return nil
		}
		return cobra.MinimumNArgs(2)(cmd, args)
	},
	ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return agent.SupportedSettingKeys(), cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if configSetListKeys {
			for _, k := range agent.SupportedSettingKeys() {
				fmt.Println(k)
			}
			return nil
		}
		key, value := args[0], strings.Join(args[1:], " ")
		if !slices.Contains(agent.SupportedSettingKeys(), strings.ToLower(key)) {
			return usageErrorWithHint(cmd, "Run 'sprout config set --help-keys' to list the settable keys.",
				"unknown setting key %q", key)
		}
		mgr, err := configSetManager()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		var valueErr error
		if err := mgr.UpdateConfig(func(c *configuration.Config) error {
			valueErr = agent.SetSettingValue(c, key, value)
			return valueErr
		}); err != nil {
			if valueErr != nil {
				return usageErrorf(cmd, "invalid value for %s: %v", key, valueErr)
			}
			return fmt.Errorf("saving %s: %w", key, err)
		}
		console.GlyphSuccess.Fprintf(os.Stderr, "%s = %s", key, value)
		return nil
	},
}

var (
	configSetListKeys bool
	configSetGlobal   bool
)

// configSetManager opens config the way createChatAgent does, so a value set
// here lands in the layer the agent reads: the workspace layer inside a
// repository, the global config otherwise or with --global.
func configSetManager() (*configuration.Manager, error) {
	globalDir := resolveGlobalConfigDir()
	switch {
	case configSetGlobal && globalDir != "":
		return configuration.NewManagerWithLayers(globalDir, "")
	case autoDetectedWorkspaceDir != "" && globalDir != "":
		return configuration.NewManagerWithLayers(globalDir, autoDetectedWorkspaceDir)
	default:
		return configuration.NewManagerSilent()
	}
}

func init() {
	configSetCmd.Flags().BoolVar(&configSetListKeys, "help-keys", false, "List the settable keys and exit")
	configSetCmd.Flags().BoolVar(&configSetGlobal, "global", false, "Write the global config even inside a repository")
	configCmd.AddCommand(configSetCmd)
}
