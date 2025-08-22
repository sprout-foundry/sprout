package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alantheprice/ledit/pkg/workspace"

	"github.com/spf13/cobra"
)

var ignoreCmd = &cobra.Command{
	Use:   "ignore [path]",
	Short: "Add a path to the leditignore file",
	Long: `Adds a path to the .ledit/leditignore file which will be used
in addition to .gitignore for determining which files to include in the workspace.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]
		leditDir := "./.ledit"
		ignoreFile := filepath.Join(leditDir, "leditignore")

		// Ensure .ledit directory exists
		if err := os.MkdirAll(leditDir, os.ModePerm); err != nil {
			fmt.Printf("Error creating .ledit directory: %v\n", err)
			return
		}

		// Add the path to leditignore
		if err := workspace.AddToLeditIgnore(ignoreFile, path); err != nil {
			fmt.Printf("Error adding to leditignore: %v\n", err)
			return
		}

		fmt.Printf("Added '%s' to %s\n", path, ignoreFile)
	},
}

func init() {
	rootCmd.AddCommand(ignoreCmd)
}
