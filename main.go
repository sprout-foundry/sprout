package main

import (
	"ledit/cmd"
	"ledit/pkg/utils" // Import the utils package

	"os" // Import os for os.Exit
)

func main() {
	// Get the logger instance
	logger := utils.GetLogger(false)
	// Defer closing the logger to ensure all buffered logs are written
	defer func() {
		if err := logger.Close(); err != nil {
			// Log the error if closing fails, but don't panic
			// Since the logger itself might be the issue, print to stderr
			os.Stderr.WriteString("Error closing logger: " + err.Error() + "\n")
		}
	}()

	if err := cmd.Execute(); err != nil {
		// Log the error before exiting
		logger.Logf("Application error: %v", err)
		os.Exit(1)
	}
}
