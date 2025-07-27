package cmd

import (
	"bytes"
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	fixModelFlag                string
	fixSkipPromptFlag           bool
	fixOptionalInstructions     string // New flag for optional instructions
	fixSkipWorkspaceContextFlag bool   // Flag to control workspace usage
)

var fixCmd = &cobra.Command{
	Use:   "fix [command]",
	Short: "Run a command, and if it fails, try to fix it with an LLM",
	Long:  `Runs a command, captures its output. If the command returns an error or produces output, it is passed to an LLM to generate a fix.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		commandToRun := args[0]

		fmt.Printf("Running command: %s\n", commandToRun)

		// Using "sh -c" to execute the command string
		c := exec.Command("sh", "-c", commandToRun)
		var outAndErr bytes.Buffer
		c.Stdout = &outAndErr
		c.Stderr = &outAndErr

		err := c.Run()

		output := strings.TrimSpace(outAndErr.String())

		if err == nil && output == "" {
			fmt.Println("Command ran successfully with no output. Nothing to fix.")
			return
		}

		fmt.Println("--- Command Output ---")
		fmt.Println(output)
		fmt.Println("----------------------")

		if err != nil {
			fmt.Printf("Command failed with error: %v\n", err)
		}

		cfg, err := config.LoadOrInitConfig(fixSkipPromptFlag)
		if err != nil {
			log.Fatalf("Failed to load configuration: %v. Please run 'ledit init'.", err)
		}

		if fixModelFlag != "" {
			cfg.EditingModel = fixModelFlag
		}
		cfg.SkipPrompt = fixSkipPromptFlag

		instructions := fmt.Sprintf("Fix the following command output: \n-------\n%s\n-------\n  making sure to include all files that have errors and files they reference. #WS", output)
		if fixSkipWorkspaceContextFlag {
			instructions = fmt.Sprintf("Fix the following command output: \n-------\n%s\n-------\n ", output)
		}

		// Prepend optional instructions if provided
		if fixOptionalInstructions != "" {
			instructions = fmt.Sprintf("%s\n\nAdditional context/instructions for the fix: %s", fixOptionalInstructions, instructions)
		}

		fmt.Println("Attempting to fix errors with LLM...")
		startTime := time.Now()

		_, err = editor.ProcessCodeGeneration("", instructions, cfg)
		if err != nil {
			log.Fatalf("Error during code generation: %v", err)
		}
		duration := time.Since(startTime)
		fmt.Printf("Fix attempt finished in %s\n", duration)
	},
}

func init() {
	fixCmd.Flags().StringVarP(&fixModelFlag, "model", "m", "", "Model name to use with the LLM")
	fixCmd.Flags().BoolVar(&fixSkipPromptFlag, "skip-prompt", false, "Skip user prompt for applying changes")
	fixCmd.Flags().BoolVar(&fixSkipWorkspaceContextFlag, "skip-workspace", false, "Skip adding the workspace context (default is false)")
	// New flag for optional instructions
	fixCmd.Flags().StringVarP(&fixOptionalInstructions, "instructions", "i", "", "Additional instructions for the LLM to consider when fixing")
}
