package utils

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"sync" // For thread-safe initialization

	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger represents a workspace logger.
type Logger struct {
	logger                 *log.Logger
	userInteractionEnabled bool // Flag to control user interaction
}

var (
	globalLogger *Logger
	once         sync.Once
)

// GetLogger returns the singleton instance of Logger.
// It initializes the logger with a file handler that rotates logs.
func GetLogger(userInteractionEnabled bool) *Logger {
	once.Do(func() {
		logFile := &lumberjack.Logger{
			Filename:   ".ledit/workspace.log",
			MaxSize:    15, // megabytes
			MaxBackups: 3,
			MaxAge:     28,   // days
			Compress:   true, // disabled by default
		}
		globalLogger = &Logger{
			logger:                 log.New(logFile, "", log.LstdFlags),
			userInteractionEnabled: userInteractionEnabled,
		}
	})
	return globalLogger
}

// Close closes the logger resources.
func (w *Logger) Close() error {
	if logFile, ok := w.logger.Writer().(*lumberjack.Logger); ok {
		return logFile.Close()
	}
	return nil
}

// LogAnalysisResult logs analysis results. These messages go only to the log file.
func (w *Logger) LogAnalysisResult(filePath, success, summary, err string) {
	w.logger.Printf("Analysis Result - File: %s, Success: %s, Summary: %s, Error: %s", filePath, success, summary, err)
}

// LogWorkspaceOperation logs workspace operations. These messages go only to the log file.
func (w *Logger) LogWorkspaceOperation(operation, details string) {
	w.logger.Printf("Operation: %s, Details: %s", operation, details)
}

// LogUserInteraction logs user interactions that require a response, and prints to stdout.
func (w *Logger) LogUserInteraction(message string) {
	w.logger.Printf("User Interaction: %s", message)
	os.Stdout.WriteString(message + "\n") // Also print to stdout
}

// LogProcessStep logs the current step in a process, and prints to stdout.
func (w *Logger) LogProcessStep(step string) {
	w.logger.Printf("Process Step: %s", step)
	os.Stdout.WriteString(step + "\n") // Also print to stdout
}

// Log logs a general message only to the log file.
func (w *Logger) Log(message string) {
	w.logger.Print(message)
}

// Logf logs a formatted general message only to the log file.
func (w *Logger) Logf(format string, v ...interface{}) {
	w.logger.Printf(format, v...)
}

func (w *Logger) LogError(err error) {
	w.logger.Printf("Error: %s", err)
}

// AskForConfirmation prompts the user with a message and waits for a 'yes' or 'no' response.
// It returns true for 'yes' and false for 'no'.
func (w *Logger) AskForConfirmation(prompt string, required bool) bool {
	if !w.userInteractionEnabled && required {
		w.LogUserInteraction("Skipping confirmation in non-interactive mode.")
		os.Exit(1) // Exit if confirmation is required but user interaction is disabled
	}
	if !w.userInteractionEnabled {
		w.LogUserInteraction("Skipping user confirmation in non-interactive mode.")
		return true // Default to true if not interactive
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		w.LogUserInteraction(fmt.Sprintf("%s (yes/no): ", prompt))
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))
		if response == "yes" || response == "y" {
			return true
		} else if response == "no" || response == "n" {
			return false
		} else {
			w.LogUserInteraction("Invalid input. Please type 'yes' or 'no'.")
		}
	}
}
