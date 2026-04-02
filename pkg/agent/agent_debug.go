package agent

import (
	"fmt"
	"os"
	"time"
)

// initDebugLogger creates a temporary file for debug logs and writes a session header
func (a *Agent) initDebugLogger() error {
	// Create temp file
	f, err := os.CreateTemp("", "ledit-debug-*.log")
	if err != nil {
		return err
	}
	a.debugLogFile = f
	a.debugLogPath = f.Name()

	// Write header
	header := fmt.Sprintf("==== Ledit Debug Log ====%sSession start: %s\nProvider: %s\nModel: %s\nPID: %d\n========================\n",
		"\n",
		time.Now().Format(time.RFC3339),
		a.GetProvider(), a.GetModel(), os.Getpid(),
	)
	a.debugLogMutex.Lock()
	defer a.debugLogMutex.Unlock()
	if _, err := a.debugLogFile.WriteString(header); err != nil {
		return err
	}
	return nil
}
