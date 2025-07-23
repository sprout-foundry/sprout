package cmd

import (
	"fmt"
	"ledit/pkg/config"
	"log"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new configuration in the current directory",
	Long:  `Creates a .ledit/config.json file in the current working directory, allowing for project-specific settings.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Initializing new configuration in the current directory...")
		if err := config.InitConfig(skipPrompt); err != nil {
			log.Fatalf("Failed to initialize configuration: %v", err)
		}
	},
}
