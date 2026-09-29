//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

var configPruneDryRun bool

var configPruneWorkspaceCmd = &cobra.Command{
	Use:   "prune-workspace [workspace...]",
	Short: "Remove settings copied from the global config into workspace config",
	Long: `Earlier versions saved the whole merged configuration into a workspace's
.sprout/workspace.json whenever a setting changed there, so later changes to
the global config stopped applying in that workspace.

prune-workspace removes every workspace setting whose value the global
config already gives. A setting you deliberately set to the same value as
the global one is removed as well; it keeps its value until the global
setting changes. The original file is kept as workspace.json.bak.

With no arguments, prunes the workspace in the current directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		roots := args
		if len(roots) == 0 {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			roots = []string{cwd}
		}
		globalDir := resolveGlobalConfigDir()
		if globalDir == "" {
			return fmt.Errorf("could not resolve the global config directory")
		}
		for _, root := range roots {
			abs, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("resolve %q: %w", root, err)
			}
			dir := configuration.WorkspaceConfigDir(abs)
			if dir == "" {
				cmd.Printf("%s: skipped (the home directory has no workspace layer)\n", abs)
				continue
			}
			file := filepath.Join(dir, configuration.WorkspaceConfigFileName)
			if _, err := os.Stat(file); os.IsNotExist(err) {
				cmd.Printf("%s: no workspace config\n", abs)
				continue
			}
			res, err := configuration.PruneWorkspaceConfig(globalDir, file, configPruneDryRun)
			if err != nil {
				return err
			}
			verb := "removed"
			if configPruneDryRun {
				verb = "would remove"
			}
			if len(res.Removed) == 0 {
				cmd.Printf("%s: nothing copied from the global config\n", file)
				continue
			}
			cmd.Printf("%s: %s %d settings; keeps %s\n", file, verb, len(res.Removed), keptSummary(res.Kept))
			for _, path := range res.Removed {
				cmd.Printf("  - %s\n", path)
			}
		}
		return nil
	},
}

func keptSummary(kept []string) string {
	var own []string
	for _, k := range kept {
		if k != "version" {
			own = append(own, k)
		}
	}
	if len(own) == 0 {
		return "nothing else"
	}
	return strings.Join(own, ", ")
}

func init() {
	configPruneWorkspaceCmd.Flags().BoolVar(&configPruneDryRun, "dry-run", false, "list what would be removed without changing anything")
	configCmd.AddCommand(configPruneWorkspaceCmd)
}
