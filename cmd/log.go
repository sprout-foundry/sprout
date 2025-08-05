package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/spf13/cobra"
)

var rawLog bool // Flag to indicate if raw verbose log should be displayed

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Print revision history or verbose log",
	Long: `Displays a log of all changes made by ledit, allowing you to review, revert, or restore them.
	Use the --raw-log flag to view the verbose internal log file.`,
	Run: func(cmd *cobra.Command, args []string) {
		if rawLog {
			displayVerboseLog()
		} else {
			if err := changetracker.PrintRevisionHistory(); err != nil {
				log.Fatalf("Failed to print revision history: %v", err)
			}
		}
	},
}

func init() {
	logCmd.Flags().BoolVar(&rawLog, "raw-log", false, "Display the raw verbose internal log file (.ledit/workspace.log)")
	rootCmd.AddCommand(logCmd)
}

// displayVerboseLog reads and interactively displays the last 100 lines of the verbose log.
func displayVerboseLog() {
	// The log file path is hardcoded in pkg/utils/logger.go
	// We need to ensure the .ledit directory exists before trying to open the log file.
	logDirPath := ".ledit"
	if _, err := os.Stat(logDirPath); os.IsNotExist(err) {
		fmt.Printf("Log directory %s does not exist. No log entries yet.\n", logDirPath)
		return
	}

	logFilePath := ".ledit/workspace.log"

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

	// Get the last 5000 lines
	const maxLinesToDisplay = 20000
	startIndex := 0
	if len(lines) > maxLinesToDisplay {
		startIndex = len(lines) - maxLinesToDisplay
	}
	displayLines := lines[startIndex:]

	const linesPerChunk = 100
	currentLineIndex := 0
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("Displaying last %d lines of %s (total %d lines available):\n", len(displayLines), logFilePath, len(lines))
	fmt.Println(strings.Repeat("=", 80))

	for {
		if currentLineIndex >= len(displayLines) {
			fmt.Println("\nEnd of log.")
			break
		}

		endIndex := currentLineIndex + linesPerChunk
		if endIndex > len(displayLines) {
			endIndex = len(displayLines)
		}

		for i := currentLineIndex; i < endIndex; i++ {
			fmt.Println(displayLines[i])
		}
		currentLineIndex = endIndex

		if currentLineIndex < len(displayLines) {
			fmt.Print("\nPress Enter to show more, or 'x' to exit: ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))
			if input == "x" || input == "exit" {
				break
			}
		} else {
			// All lines displayed, no need to prompt for more
			break
		}
	}
	fmt.Println(strings.Repeat("=", 80))
}
