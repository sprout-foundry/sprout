package main

// main.go is the entry point for the ledit CLI application.

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/cmd"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/providers"
	"github.com/alantheprice/ledit/pkg/utils"
)

func main() {
	// Initialize the new modular provider system
	logger := utils.GetLogger(true)

	// Register all default providers (OpenAI, Gemini, Ollama, etc.)
	if err := providers.RegisterDefaultProviders(); err != nil {
		if logger != nil {
			logger.LogError(fmt.Errorf("failed to register default providers: %w", err))
		}
		
		// Create a graceful exit message for provider registration failure
		exitMsg := prompts.GracefulExitMessage{
			Context: "Initializing the LLM provider system",
			Error:   err,
			Accomplished: []string{
				"Started application",
			},
			Resolution: []string{
				"Check provider configuration files",
				"Verify API keys are properly set",
				"Ensure all provider dependencies are available",
			},
		}

		if _, printErr := fmt.Fprintln(os.Stderr, prompts.GracefulExit(exitMsg)); printErr != nil {
		}
		os.Exit(1)
	}

	if err := prompts.InitPromptManager(); err != nil {
		if logger != nil {
			logger.LogError(fmt.Errorf("failed to initialize prompt manager: %w", err))
		}

		// Create a graceful exit message for prompt manager failure
		exitMsg := prompts.GracefulExitMessage{
			Context: "Initializing the prompt management system",
			Error:   err,
			Accomplished: []string{
				"Started application",
			},
			Resolution: []string{
				"Check if the home directory is accessible",
				"Verify file system permissions",
				"Ensure the .ledit directory can be created",
			},
		}

		if _, printErr := fmt.Fprintln(os.Stderr, prompts.GracefulExit(exitMsg)); printErr != nil {
		}
		os.Exit(1)
	}

	if logger == nil {
		// Create a graceful exit message for logger failure
		exitMsg := prompts.GracefulExitMessage{
			Context: "Initializing the logging system",
			Error:   fmt.Errorf("failed to initialize logger"),
			Accomplished: []string{
				"Started application",
			},
			Resolution: []string{
				"Check system resources and permissions",
				"Verify the application installation",
			},
		}

		if _, printErr := fmt.Fprintln(os.Stderr, prompts.GracefulExit(exitMsg)); printErr != nil {
		}
		os.Exit(1)
	}

	defer func() {
		if err := logger.Close(); err != nil {
			if _, writeErr := os.Stderr.WriteString("Error closing logger: " + err.Error() + "\n"); writeErr != nil {
			}
		}
	}()

	if err := cmd.Execute(); err != nil {
		logger.LogError(err)

		// Create a graceful exit message with context
		exitMsg := prompts.GracefulExitMessage{
			Context: "Processing your CLI command",
			Error:   err,
			Accomplished: []string{
				"Initialized prompt manager",
				"Set up logging system",
			},
			Resolution: []string{
				"Check the command syntax and arguments",
				"Verify file permissions if applicable",
				"Review the workspace log for more details",
			},
		}

		if _, printErr := fmt.Fprintln(os.Stderr, prompts.GracefulExit(exitMsg)); printErr != nil {
		}
		os.Exit(1)
	}
}

func multiply(a int, b int) int {
	return a * b
}