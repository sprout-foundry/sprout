package cmd

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Regenerate project validation context",
	Long:  `Analyze the current project and regenerate validation context for the agent.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Analyzing project for validation requirements...")

		if err := agent.RegenerateProjectValidationContext(); err != nil {
			fmt.Printf("Error generating validation context: %v\n", err)
			return
		}

		fmt.Println("✓ Validation context regenerated successfully")
		fmt.Println("  Saved to: .ledit/validation_context.md")
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
