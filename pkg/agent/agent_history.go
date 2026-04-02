package agent

import (
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// AddToHistory adds a command to the history buffer
func (a *Agent) AddToHistory(command string) {
	// Don't add empty commands or duplicates of the last command
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}

	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	// Remove from history if it already exists (to avoid duplicates)
	for i, cmd := range a.commandHistory {
		if cmd == command {
			a.commandHistory = append(a.commandHistory[:i], a.commandHistory[i+1:]...)
			break
		}
	}

	// Add to history
	a.commandHistory = append(a.commandHistory, command)

	// Limit history size
	if len(a.commandHistory) > 100 {
		a.commandHistory = a.commandHistory[1:]
	}

	// Reset history index to end
	a.historyIndex = -1

	// Save history to configuration for persistence
	// saveHistoryToConfig reads commandHistory/historyIndex directly;
	// caller (AddToHistory) already holds historyMu.
	a.saveHistoryToConfig()
}

// GetHistoryCommand returns the command at the given index from history
func (a *Agent) GetHistoryCommand(index int) string {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	if index < 0 || index >= len(a.commandHistory) {
		return ""
	}
	return a.commandHistory[index]
}

// NavigateHistory navigates through command history
// direction: 1 for up (older), -1 for down (newer)
// currentIndex: current position in the input line
func (a *Agent) NavigateHistory(direction int, currentIndex int) (string, int) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	if len(a.commandHistory) == 0 {
		return "", currentIndex
	}

	switch direction {
	case 1: // Up arrow - go to older commands
		if a.historyIndex == -1 {
			// Starting from current input, go to last command
			a.historyIndex = len(a.commandHistory) - 1
		} else if a.historyIndex > 0 {
			// Go to older command
			a.historyIndex--
		}
	case -1: // Down arrow - go to newer commands
		if a.historyIndex == -1 {
			// Already at newest, return empty
			return "", currentIndex
		} else if a.historyIndex < len(a.commandHistory)-1 {
			// Go to newer command
			a.historyIndex++
		} else {
			// At the newest command, reset to current input
			a.historyIndex = -1
			return "", currentIndex
		}
	}

	if a.historyIndex == -1 {
		return "", currentIndex
	}

	return a.commandHistory[a.historyIndex], currentIndex
}

// ResetHistoryIndex resets the history navigation index
func (a *Agent) ResetHistoryIndex() {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	a.historyIndex = -1
}

// GetHistorySize returns the number of commands in history
func (a *Agent) GetHistorySize() int {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	return len(a.commandHistory)
}

// GetHistory returns a defensive copy of the command history.
func (a *Agent) GetHistory() []string {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	result := make([]string, len(a.commandHistory))
	copy(result, a.commandHistory)
	return result
}

// loadHistoryFromConfig loads command history from the configuration
func (a *Agent) loadHistoryFromConfig() {
	if a.configManager == nil {
		return
	}

	config := a.configManager.GetConfig()
	if config == nil {
		return
	}

	pathKey := a.historyPathKey()
	if len(config.CommandHistoryByPath) > 0 {
		if history, ok := config.CommandHistoryByPath[pathKey]; ok && len(history) > 0 {
			a.historyMu.Lock()
			a.commandHistory = append([]string(nil), history...)
			a.historyIndex = -1
			a.historyMu.Unlock()
			return
		}
	}
}

// saveHistoryToConfig saves command history to the configuration
func (a *Agent) saveHistoryToConfig() {
	if a.configManager == nil {
		return
	}

	if err := a.configManager.UpdateConfig(func(config *configuration.Config) error {
		if config.CommandHistoryByPath == nil {
			config.CommandHistoryByPath = make(map[string][]string)
		}
		if config.HistoryIndexByPath == nil {
			config.HistoryIndexByPath = make(map[string]int)
		}

		pathKey := a.historyPathKey()
		if len(a.commandHistory) == 0 {
			delete(config.CommandHistoryByPath, pathKey)
			delete(config.HistoryIndexByPath, pathKey)
		} else {
			config.CommandHistoryByPath[pathKey] = append([]string(nil), a.commandHistory...)
			config.HistoryIndexByPath[pathKey] = a.historyIndex
		}
		return nil
	}); err != nil && a.debug {
		a.debugLog("Failed to save command history to config: %v\n", err)
	}
}

func (a *Agent) historyPathKey() string {
	root := a.currentWorkspaceRoot()
	if strings.TrimSpace(root) == "" || root == "." {
		return "unknown"
	}
	cleaned := filepath.Clean(root)
	abs, err := filepath.Abs(cleaned)
	if err == nil && strings.TrimSpace(abs) != "" {
		return filepath.Clean(abs)
	}
	return cleaned
}
