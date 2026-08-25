package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// Logger provides structured logging functionality
type Logger struct {
	logFile *os.File
}

// NewLogger creates a new logger instance
func NewLogger() (*Logger, error) {
	logger := &Logger{}
	err := logger.init()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}
	return logger, nil
}

// init initializes the logging system
func (l *Logger) init() error {
	stateDir, err := envutil.StateDir()
	if err != nil {
		return fmt.Errorf("failed to resolve state directory: %w", err)
	}
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file for writing
	logPath := filepath.Join(logDir, "sprout.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	l.logFile = logFile

	return nil
}

// Log writes a log message with timestamp
func (l *Logger) Log(level string, format string, args ...interface{}) {
	if l.logFile == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] [%s] %s\n", timestamp, strings.ToUpper(level), message)

	// Write to file
	_, err := l.logFile.WriteString(logLine)
	if err != nil {
		// If we can't write to log file, at least print to stderr
		fmt.Fprintf(os.Stderr, "Failed to write to log: %v\n", err)
	}

	// Also write to stdout for immediate visibility
	fmt.Print(logLine)
}

// Debug logs debug information
func (l *Logger) Debug(format string, args ...interface{}) {
	l.Log("debug", format, args...)
}

// Info logs informational messages
func (l *Logger) Info(format string, args ...interface{}) {
	l.Log("info", format, args...)
}

// Warn logs warning messages
func (l *Logger) Warn(format string, args ...interface{}) {
	l.Log("warn", format, args...)
}

// Error logs error messages
func (l *Logger) Error(format string, args ...interface{}) {
	l.Log("error", format, args...)
}

// Close closes the log file
func (l *Logger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// WriteLocalCopy writes a copy of content to a local diagnostic file for debugging
func WriteLocalCopy(filename string, content []byte) {
	cacheDir, err := envutil.CacheDir()
	if err != nil {
		return
	}
	diagDir := filepath.Join(cacheDir, "diagnostics")
	_ = os.MkdirAll(diagDir, 0o755)
	logPath := filepath.Join(diagDir, filename)

	if err := os.WriteFile(logPath, content, 0600); err != nil {
		fmt.Printf("Failed to write local copy: %v\n", err)
	}
}

// GetLogPath returns the path to the log file in the state directory.
// Does not create directories — pure path computation.
func GetLogPath() string {
	if v := os.Getenv("SPROUT_STATE_DIR"); v != "" {
		return filepath.Join(v, "logs", "sprout.log")
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "sprout", "logs", "sprout.log")
	}
	home := os.Getenv("HOME")
	if home == "" {
		hd, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "sprout.log")
		}
		home = hd
	}
	return filepath.Join(home, ".local", "state", "sprout", "logs", "sprout.log")
}
