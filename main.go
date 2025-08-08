package main

import (
	"github.com/alantheprice/ledit/cmd"
	"github.com/alantheprice/ledit/pkg/utils" // Import the utils package

	"os" // Import os for os.Exit
)

func main() {
	// Get the logger instance
	logger := utils.GetLogger(true)
	// Defer closing the logger to ensure all buffered logs are written
	defer func() {
		if err := logger.Close(); err != nil {
			// Log the error if closing fails, but don't panic
			// Since the logger itself might be the issue, print to stderr
			os.Stderr.WriteString("Error closing logger: " + err.Error() + "\n")
		}
	}()

	cmd.Execute()
}
