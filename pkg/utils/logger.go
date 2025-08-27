package utils

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync" // For thread-safe initialization

	ui "github.com/alantheprice/ledit/pkg/ui"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger represents a workspace logger.
type Logger struct {
	logger                 *log.Logger
	userInteractionEnabled bool // Flag to control user interaction
	jsonMode               bool
	correlationID          string
}

var (
	globalLogger *Logger
	once         sync.Once
)

// GetLogger returns the singleton instance of Logger.
// It initializes the logger with a file handler that rotates logs.
// The skipPrompts parameter determines if user interaction is enabled.
// This value can be overridden on subsequent calls to GetLogger.
func GetLogger(skipPrompts bool) *Logger {
	once.Do(func() {
		logFile := &lumberjack.Logger{
			Filename:   ".ledit/workspace.log",
			MaxSize:    15, // megabytes
			MaxBackups: 3,
			MaxAge:     28,   // days
			Compress:   true, // disabled by default
		}
		globalLogger = &Logger{
			logger: log.New(logFile, "", log.LstdFlags),
			// userInteractionEnabled will be set below, after the once.Do block
		}
	})
	// Always update userInteractionEnabled, allowing it to be overridden
	globalLogger.userInteractionEnabled = !skipPrompts
	if os.Getenv("LEDIT_JSON_LOGS") == "1" {
		globalLogger.jsonMode = true
	}
	if cid := os.Getenv("LEDIT_CORRELATION_ID"); cid != "" {
		globalLogger.correlationID = cid
	}
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
	ui.Out().Print(message + "\n")
}

// LogProcessStep logs the current step in a process, with smart UI filtering.
func (w *Logger) LogProcessStep(step string) {
	w.logger.Printf("Process Step: %s", step)
	// Use smart logging to determine if this should be shown in UI
	ui.SmartLog(step)
}

// Log logs a general message only to the log file.
func (w *Logger) Log(message string) {
	if w.jsonMode {
		_ = json.NewEncoder(w.logger.Writer()).Encode(map[string]any{"level": "info", "msg": message, "cid": w.correlationID})
		return
	}
	w.logger.Print(message)
}

// Logf logs a formatted general message only to the log file.
func (w *Logger) Logf(format string, v ...interface{}) {
	if w.jsonMode {
		w.Log(fmt.Sprintf(format, v...))
		return
	}
	w.logger.Printf(format, v...)
}

func (w *Logger) LogError(err error) {
	if w.jsonMode {
		_ = json.NewEncoder(w.logger.Writer()).Encode(map[string]any{"level": "error", "error": err.Error(), "cid": w.correlationID})
		return
	}
	w.logger.Printf("Error: %s", err)
}

// AskForConfirmation prompts the user with a message and waits for a 'yes' or 'no' response.
// It returns true for 'yes' and false for 'no'.
func (w *Logger) AskForConfirmation(prompt string, default_response bool, required bool) bool {
	if !w.userInteractionEnabled && required {
		w.Log("User interaction is disabled, but confirmation is required.")
		w.Log(fmt.Sprintf("We were going to ask the user: '%s'", prompt))
		w.Log("Exiting due to lack of confirmation in prompt-skipping mode.")
		os.Exit(1) // Exit if confirmation is required but user interaction is disabled
	}
	if !w.userInteractionEnabled {
		w.Log("Skipping user confirmation in non-interactive mode.")
		return default_response
	}
	// If UI is enabled, ask via TUI prompt events, but only if user interaction is enabled
	if ui.IsUIActive() {
		confirmed, err := ui.PromptYesNo(prompt, default_response)
		if err != nil {
			return default_response
		}
		return confirmed
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		w.LogUserInteraction(fmt.Sprintf("%s (yes/no): ", prompt))
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))
		switch response {
		case "yes", "y":
			return true
		case "no", "n":
			return false
		default:
			w.LogUserInteraction("Invalid input. Please type 'yes' or 'no'.")
		}
	}
}
