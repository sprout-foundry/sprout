package cmd

import (
	"fmt"
	"log"

	"github.com/alantheprice/ledit/pkg/config"

	"github.com/spf13/cobra"
)

// TODO: Migrate to new BaseCommand framework
var initSkipPrompt bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new configuration in the current directory",
	Long:  `Creates a .ledit/config.json file in the current working directory, allowing for project-specific settings.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Initializing new configuration in the current directory...")
		if err := config.InitConfig(initSkipPrompt); err != nil {
			log.Fatalf("Failed to initialize configuration: %v", err)
		}
	},
}

func init() {
	initCmd.Flags().BoolVar(&initSkipPrompt, "skip-prompt", false, "Skip user confirmation prompts")
}
