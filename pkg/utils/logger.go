package utils

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync" // For thread-safe initialization

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/envutil"
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
	loggerMu     sync.RWMutex
)

// GetLogger returns the singleton instance of Logger.
// It initializes the logger with a file handler that rotates logs.
// The skipPrompts parameter determines if user interaction is enabled.
// This value can be overridden on subsequent calls to GetLogger.
func GetLogger(skipPrompts bool) *Logger {
	once.Do(func() {
		logFile := &lumberjack.Logger{
			Filename:   ".sprout/workspace.log",
			MaxSize:    15, // megabytes
			MaxBackups: 3,
			MaxAge:     28,   // days
			Compress:   true, // disabled by default
		}
		globalLogger = &Logger{
			logger:                 log.New(logFile, "", log.LstdFlags),
			userInteractionEnabled: !skipPrompts,
		}
	})
	// Always update userInteractionEnabled, allowing it to be overridden
	loggerMu.Lock()
	globalLogger.userInteractionEnabled = !skipPrompts
	if envutil.GetEnvSimple("JSON_LOGS") == "1" {
		globalLogger.jsonMode = true
	}
	if cid := envutil.GetEnvSimple("CORRELATION_ID"); cid != "" {
		globalLogger.correlationID = cid
	}
	loggerMu.Unlock()
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
	fmt.Print(message + "\n")
}

// LogProcessStep logs the current step in a process.
func (w *Logger) LogProcessStep(step string) {
	w.logger.Printf("Process Step: %s", step)
	// Print process step to stdout
	fmt.Printf("Step: %s\n", step)
}

// Log logs a general message only to the log file.
func (w *Logger) Log(message string) {
	loggerMu.RLock()
	jm := w.jsonMode
	cid := w.correlationID
	loggerMu.RUnlock()
	if jm {
		_ = json.NewEncoder(w.logger.Writer()).Encode(map[string]any{"level": "info", "msg": message, "cid": cid})
		return
	}
	w.logger.Print(message)
}

// Logf logs a formatted general message only to the log file.
func (w *Logger) Logf(format string, v ...interface{}) {
	loggerMu.RLock()
	jm := w.jsonMode
	loggerMu.RUnlock()
	if jm {
		w.Log(fmt.Sprintf(format, v...))
		return
	}
	w.logger.Printf(format, v...)
}

func (w *Logger) LogError(err error) {
	loggerMu.RLock()
	jm := w.jsonMode
	cid := w.correlationID
	loggerMu.RUnlock()
	if jm {
		_ = json.NewEncoder(w.logger.Writer()).Encode(map[string]any{"level": "error", "error": err.Error(), "cid": cid})
		return
	}
	w.logger.Printf("Error: %s", err)
}

// AskForConfirmation prompts the user with a message and waits for a 'yes' or 'no' response.
// It returns true for 'yes' and false for 'no'.
func (w *Logger) AskForConfirmation(prompt string, default_response bool, required bool) bool {
	loggerMu.RLock()
	interactive := w.userInteractionEnabled
	loggerMu.RUnlock()
	if !interactive && required {
		w.Log("User interaction is disabled, but confirmation is required.")
		w.Log(fmt.Sprintf("We were going to ask the user: '%s'", prompt))
		w.Log("Exiting due to lack of confirmation in prompt-skipping mode.")
		os.Exit(1) // Exit if confirmation is required but user interaction is disabled
	}
	if !interactive {
		w.Log("Skipping user confirmation in non-interactive mode.")
		return default_response
	}
	// SP-048 follow-up: stop any active CLI spinner so the prompt isn't
	// overwritten by spinner frames on stderr.
	clihooks.SuspendIndicator()
	// SP-057 follow-up: pause the SP-055 SteerInputReader so it releases
	// stdin back to cooked mode. Without this, the bufio.Reader below
	// hits EOF immediately (the steer reader is holding raw-mode stdin),
	// trips the consecutive-error guard, and auto-rejects the prompt
	// with "stdin unavailable - rejecting for safety". No-op when no
	// turn is in flight.
	clihooks.PauseSteer()
	defer clihooks.ResumeSteer()
	reader := bufio.NewReader(os.Stdin)
	consecutiveErrors := 0
	const maxConsecutiveErrors = 3

	// SP-048-4c: render the prompt with the default option in bold so the
	// user can hit Enter for the safe choice. Honors NO_COLOR / FORCE_COLOR
	// via the shared color-preference resolver in pkg/console.
	hint := DefaultChoiceHint(default_response)

	for {
		w.LogUserInteraction(fmt.Sprintf("%s %s: ", prompt, hint))
		response, err := ReadLineWithTimeout(reader, ApprovalPromptTimeout)

		// An idle (open but silent) stdin means the user walked away or the
		// harness isn't forwarding keystrokes. Deny rather than block the
		// agent forever; the consecutive-error guard below only catches a
		// *closed* stdin, not a stalled one.
		if errors.Is(err, ErrPromptTimeout) {
			w.LogUserInteraction(fmt.Sprintf(" timed out after %s waiting for input - rejecting for safety.", ApprovalPromptTimeout))
			return false
		}

		// Handle EOF or read errors - these indicate stdin is closed/unavailable
		// Without this check, we'd loop infinitely printing the prompt
		if err != nil {
			consecutiveErrors++
			w.Log(fmt.Sprintf("AskForConfirmation: read error (attempt %d/%d): %v", consecutiveErrors, maxConsecutiveErrors, err))

			if consecutiveErrors >= maxConsecutiveErrors {
				w.LogUserInteraction(" stdin unavailable - rejecting for safety.")
				return false // Reject for safety when stdin is unavailable
			}
			continue
		}

		// Reset error counter on successful read
		consecutiveErrors = 0

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

// IsInteractive returns true if user interaction is enabled
func (w *Logger) IsInteractive() bool {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return w.userInteractionEnabled
}

// DefaultChoiceHint builds the "[Y/n]" or "[y/N]" tail for a confirmation
// prompt, with the default letter rendered in bold ANSI when color output
// is allowed (honors NO_COLOR / FORCE_COLOR via console.ResolveColorPreference).
// Hitting Enter on an empty response is currently rejected by the loop, so
// the visual hint also communicates to the user that the capitalized
// letter is the safe choice to type explicitly.
func DefaultChoiceHint(defaultYes bool) string {
	yes, no := "y", "n"
	if defaultYes {
		yes = "Y"
	} else {
		no = "N"
	}
	if envutil.ResolveColorPreference(true) {
		if defaultYes {
			yes = "\033[1m" + yes + "\033[0m"
		} else {
			no = "\033[1m" + no + "\033[0m"
		}
	}
	return "[" + yes + "/" + no + "]"
}
