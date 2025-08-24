package cmd

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/spf13/cobra"
)

// rollbackCmd represents the rollback command
var rollbackCmd = &cobra.Command{
	Use:   "rollback [revision-id]",
	Short: "Rollback changes by revision ID",
	Long: `Rollback changes made by the agent by specifying a revision ID.
If no revision ID is provided, shows the revision history.

Examples:
  ledit rollback                    # Show revision history
  ledit rollback abc123def         # Rollback specific revision
  ledit rollback --list            # List all revisions`,
	Run: func(cmd *cobra.Command, args []string) {
		listFlag, _ := cmd.Flags().GetBool("list")

		if listFlag || len(args) == 0 {
			// Show revision history
			fmt.Println("📋 Revision History:")
			fmt.Println(strings.Repeat("-", 50))
			if err := changetracker.PrintRevisionHistory(); err != nil {
				fmt.Printf("❌ Error listing revisions: %v\n", err)
				return
			}

			if len(args) == 0 && !listFlag {
				fmt.Println("\n💡 To rollback a specific revision, use: ledit rollback <revision-id>")
			}
			return
		}

		revisionID := args[0]

		// Check if revision has active changes
		hasActive, err := changetracker.HasActiveChangesForRevision(revisionID)
		if err != nil {
			fmt.Printf("❌ Error checking revision %s: %v\n", revisionID, err)
			return
		}

		if !hasActive {
			fmt.Printf("ℹ️ Revision %s has no active changes to rollback\n", revisionID)
			return
		}

		// Confirm rollback
		confirmFlag, _ := cmd.Flags().GetBool("yes")
		if !confirmFlag {
			fmt.Printf("🔄 About to rollback revision: %s\n", revisionID)
			fmt.Print("Are you sure? (y/N): ")

			var response string
			fmt.Scanln(&response)
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
				fmt.Println("❌ Rollback cancelled")
				return
			}
		}

		// Perform rollback
		fmt.Printf("🔄 Rolling back revision: %s\n", revisionID)
		if err := changetracker.RevertChangeByRevisionID(revisionID); err != nil {
			fmt.Printf("❌ Failed to rollback revision %s: %v\n", revisionID, err)
			return
		}

		fmt.Printf("✅ Successfully rolled back revision: %s\n", revisionID)
	},
}

func init() {
	rootCmd.AddCommand(rollbackCmd)

	rollbackCmd.Flags().BoolP("list", "l", false, "List all revisions")
	rollbackCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}
