package cmd

import (
	"ledit/pkg/changetracker"
	"log"

	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Print revision history",
	Long:  `Displays a log of all changes made by ledit, allowing you to review, revert, or restore them.`,
	Run: func(cmd *cobra.Command, args []string) {

		if err := changetracker.PrintRevisionHistory(); err != nil {
			log.Fatalf("Failed to print revision history: %v", err)
		}
	},
}
