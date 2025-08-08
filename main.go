// ledit is a command-line tool designed for managing and editing LED configurations.
// It provides various commands to interact with LED setups.
package main

import (
	"fmt" // Import fmt for printing to stderr

	"github.com/alantheprice/ledit/cmd"
	"github.com/alantheprice/ledit/pkg/prompts" // Import prompts for user-friendly error messages
	"github.com/alantheprice/ledit/pkg/utils"   // Import the utils package

	"os" // Import os for os.Exit
)

func main() {
	// Get the logger instance
	logger := utils.GetLogger(true)

	if logger == nil {
		// If the logger cannot be initialized, we cannot log, so print to stderr and exit.
		if _, err := fmt.Fprintln(os.Stderr, "FATAL: Failed to initialize logger. Exiting."); err != nil {
			// If even printing to stderr fails, there's little else we can do.
			// This is an extremely rare and severe failure.
		}
		os.Exit(1)
	}

	// Defer closing the logger to ensure all buffered logs are written
	defer func() {
		if err := logger.Close(); err != nil {
			// Log the error if closing fails, but don't panic
			// Since the logger itself might be the issue, print to stderr
			// BUG FIX: Check error from os.Stderr.WriteString, though typically not critical.
			if _, writeErr := os.Stderr.WriteString("Error closing logger: " + err.Error() + "\n"); writeErr != nil {
				// If even writing to stderr fails, there's little else we can do.
				// This is an extremely rare and severe failure.
			}
		}
	}()

	if err := cmd.Execute(); err != nil {
		logger.LogError(err) // Log the error using the logger
		// Print a user-friendly message to standard error
		// BUG FIX: Check error from fmt.Fprintln, though typically not critical.
		if _, printErr := fmt.Fprintln(os.Stderr, prompts.FatalError(err)); printErr != nil {
			// If even printing to stderr fails, there's little else we can do.
			// This is an extremely rare and severe failure.
		}
		os.Exit(1)
	}
}