package agent

import (
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// AddToHistory adds a command to the history buffer
func (a *Agent) AddToHistory(command string) {
	// Don't add empty commands or duplicates of the last command
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}

	mu := a.state.GetHistoryMutex()
	mu.Lock()

	// Get current command history
	history := a.state.GetCommandHistory()

	// Remove from history if it already exists (to avoid duplicates)
	for i, cmd := range history {
		if cmd == command {
			history = append(history[:i], history[i+1:]...)
			break
		}
	}

	// Add to history
	history = append(history, command)

	// Limit history size
	if len(history) > 100 {
		history = history[1:]
	}

	// Reset history index to end and save
	a.state.SetHistoryIndex(-1)
	a.state.SetCommandHistory(history)

	// Release lock before calling saveHistoryToConfig (which expects to hold the lock)
	mu.Unlock()

	// Save history to configuration for persistence
	a.saveHistoryToConfig()
}

// GetHistoryCommand returns the command at the given index from history
func (a *Agent) GetHistoryCommand(index int) string {
	history := a.state.GetCommandHistory()
	if index < 0 || index >= len(history) {
		return ""
	}
	return history[index]
}

// NavigateHistory navigates through command history
// direction: 1 for up (older), -1 for down (newer)
// currentIndex: current position in the input line
func (a *Agent) NavigateHistory(direction int, currentIndex int) (string, int) {
	mu := a.state.GetHistoryMutex()
	mu.Lock()
	defer mu.Unlock()

	history := a.state.GetCommandHistory()
	historyIndex := a.state.GetHistoryIndex()

	if len(history) == 0 {
		return "", currentIndex
	}

	switch direction {
	case 1: // Up arrow - go to older commands
		if historyIndex == -1 {
			// Starting from current input, go to last command
			historyIndex = len(history) - 1
		} else if historyIndex > 0 {
			// Go to older command
			historyIndex--
		}
	case -1: // Down arrow - go to newer commands
		if historyIndex == -1 {
			// Already at newest, return empty
			return "", currentIndex
		} else if historyIndex < len(history)-1 {
			// Go to newer command
			historyIndex++
		} else {
			// At the newest command, reset to current input
			historyIndex = -1
			return "", currentIndex
		}
	}

	a.state.SetHistoryIndex(historyIndex)
	if historyIndex == -1 {
		return "", currentIndex
	}

	return history[historyIndex], currentIndex
}

// ResetHistoryIndex resets the history navigation index
func (a *Agent) ResetHistoryIndex() {
	mu := a.state.GetHistoryMutex()
	mu.Lock()
	defer mu.Unlock()
	a.state.SetHistoryIndex(-1)
}

// GetHistorySize returns the number of commands in history
func (a *Agent) GetHistorySize() int {
	mu := a.state.GetHistoryMutex()
	mu.Lock()
	defer mu.Unlock()
	return len(a.state.GetCommandHistory())
}

// GetHistory returns a defensive copy of the command history.
func (a *Agent) GetHistory() []string {
	mu := a.state.GetHistoryMutex()
	mu.Lock()
	defer mu.Unlock()
	history := a.state.GetCommandHistory()
	result := make([]string, len(history))
	copy(result, history)
	return result
}

// loadHistoryFromConfig loads command history from the configuration
func (a *Agent) loadHistoryFromConfig() {
	a.initSubManagers()
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
			mu := a.state.GetHistoryMutex()
			mu.Lock()
			defer mu.Unlock()
			a.state.SetCommandHistory(append([]string(nil), history...))
			a.state.SetHistoryIndex(-1)
			return
		}
	}
}

// saveHistoryToConfig saves command history to the configuration.
// Thread-safe: reads state via getter methods and persists through
// configManager.UpdateConfig. No external lock required.
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
		history := a.state.GetCommandHistory()
		historyIndex := a.state.GetHistoryIndex()

		if len(history) == 0 {
			delete(config.CommandHistoryByPath, pathKey)
			delete(config.HistoryIndexByPath, pathKey)
		} else {
			config.CommandHistoryByPath[pathKey] = append([]string(nil), history...)
			config.HistoryIndexByPath[pathKey] = historyIndex
		}
		return nil
	}); err != nil && a.debug {
		a.Logger().Debug("Failed to save command history to config: %v\n", err)
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
