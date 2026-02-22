package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
		return nil, err
	}
	return logger, nil
}

// init initializes the logging system
func (l *Logger) init() error {
	// Create .ledit directory if it doesn't exist
	leditDir := filepath.Join(os.Getenv("HOME"), ".ledit")
	if err := os.MkdirAll(leditDir, 0755); err != nil {
		return fmt.Errorf("failed to create .ledit directory: %w", err)
	}

	// Open log file for writing
	logPath := filepath.Join(leditDir, "ledit.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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

// WriteLocalCopy writes a copy of content to a local log file for debugging
func WriteLocalCopy(filename string, content []byte) {
	leditDir := filepath.Join(os.Getenv("HOME"), ".ledit")
	logPath := filepath.Join(leditDir, filename)

	if err := os.WriteFile(logPath, content, 0644); err != nil {
		fmt.Printf("Failed to write local copy: %v\n", err)
	}
}

// GetLogPath returns the path to the log file
func GetLogPath() string {
	leditDir := filepath.Join(os.Getenv("HOME"), ".ledit")
	return filepath.Join(leditDir, "ledit.log")
}
