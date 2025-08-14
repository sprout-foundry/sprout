package main

import (
    "fmt"

	"github.com/alantheprice/ledit/cmd"
    "github.com/alantheprice/ledit/pkg/prompts"
    "github.com/alantheprice/ledit/pkg/utils"
    "os"
)

func main() {
	logger := utils.GetLogger(true)

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
