package main

// main.go is the entry point for the ledit CLI application.

import (
	"fmt"

	"os"

	"github.com/alantheprice/ledit/cmd"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

func main() {
	// TODO: Add a comment here
	logger := utils.GetLogger(true)

	if err := prompts.InitPromptManager(); err != nil {
		if logger != nil {
			logger.LogError(fmt.Errorf("failed to initialize prompt manager: %w", err))
		}
		if _, err := fmt.Fprintf(os.Stderr, "FATAL: Failed to initialize prompt manager: %v\n", err); err != nil {
		}
		os.Exit(1)
	}

	if logger == nil {
		if _, err := fmt.Fprintln(os.Stderr, "FATAL: Failed to initialize logger. Exiting."); err != nil {
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
		if _, printErr := fmt.Fprintln(os.Stderr, prompts.FatalError(err)); printErr != nil {
		}
		os.Exit(1)
	}
}
