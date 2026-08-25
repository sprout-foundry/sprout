//go:build !js

package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

var rawLog bool // Flag to indicate if raw verbose log should be displayed

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Print revision history or verbose log",
	Long: `Displays a log of all changes made by sprout, allowing you to review, revert, or restore them.
	Use the --raw-log flag to view the verbose internal log file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if rawLog {
			displayVerboseLog()
			return nil
		}
		return history.PrintRevisionHistory()
	},
}

func init() {
	logCmd.Flags().BoolVar(&rawLog, "raw-log", false, "Display the raw verbose internal log file (.sprout/workspace.log)")
}

// displayVerboseLog reads and displays the verbose log to a buffer for seamless console experience
func displayVerboseLog() {
	// The log file path is hardcoded in pkg/utils/logger.go
	// We need to ensure the .sprout directory exists before trying to open the log file.
	logDirPath := ".sprout"
	if _, err := os.Stat(logDirPath); os.IsNotExist(err) {
		fmt.Printf("Log directory %s does not exist. No log entries yet.\n", logDirPath)
		return
	}

	logFilePath := ".sprout/workspace.log"

	file, err := os.Open(logFilePath)
	if os.IsNotExist(err) {
		fmt.Printf("Verbose log file not found at %s. No log entries yet.\n", logFilePath)
		return
	}
	if err != nil {
		log.Fatalf("Failed to open verbose log file %s: %v", logFilePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Failed to read verbose log file: %v", err)
	}

	if len(lines) == 0 {
		fmt.Println("Verbose log file is empty.")
		return
	}

	// Get the last 20000 lines
	const maxLinesToDisplay = 20000
	startIndex := 0
	if len(lines) > maxLinesToDisplay {
		startIndex = len(lines) - maxLinesToDisplay
	}
	displayLines := lines[startIndex:]

	// Write to buffer instead of interactive display
	var buffer strings.Builder
	buffer.WriteString(fmt.Sprintf("Displaying last %d lines of %s (total %d lines available):\n", len(displayLines), logFilePath, len(lines)))
	buffer.WriteString(strings.Repeat("=", 80) + "\n")

	for _, line := range displayLines {
		// Redact credential patterns from log lines
		redactedLine := secretdetect.RedactOpaque(line)
		buffer.WriteString(redactedLine + "\n")
	}

	buffer.WriteString(strings.Repeat("=", 80) + "\n")

	// Output the entire buffer at once
	fmt.Print(buffer.String())
}
