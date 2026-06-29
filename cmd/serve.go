//go:build !js

package cmd

import (
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the WebUI server with a mock LLM (alias for 'agent --mock-llm')",
	Long: `Start the WebUI server with a deterministic mock LLM backend.

This is an alias for 'sprout agent --mock-llm --daemon'. Use this for E2E
tests that need a stable sprout backend without a real LLM provider.

The mock LLM returns canned responses based on the user prompt:
- "list files" / "ls" → stub ls output
- "echo" → stub echo response
- custom responses can be registered via the mock provider API
- default → echoes back the first 100 chars of the user message`,
	RunE: func(cmd *cobra.Command, args []string) error {
		agentMockLLM = true
		daemonMode = true
		return agentCmd.RunE(agentCmd, args)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
