package components

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// InputState represents the navigation state within command history
// This includes the current position in history and temporary input storage
// when browsing through command history with arrow keys
type InputState struct {
	HistoryIndex int    `json:"historyIndex"` // Current position in history (-1 = not in history mode)
	TempInput    string `json:"tempInput"`    // Temporary storage for current input when browsing history
}

// InputStateManager manages persistence of input navigation state
type InputStateManager struct {
	mutex sync.RWMutex
	state InputState
	file  string
}

// NewInputStateManager creates a new input state manager
func NewInputStateManager(filename string) *InputStateManager {
	return &InputStateManager{
		state: InputState{
			HistoryIndex: -1, // Start not in history mode
			TempInput:    "",
		},
		file: filename,
	}
}

// GetState returns a copy of the current input state
func (ism *InputStateManager) GetState() InputState {
	ism.mutex.RLock()
	defer ism.mutex.RUnlock()
	return ism.state
}

// SetState sets the current input state
func (ism *InputStateManager) SetState(state InputState) {
	ism.mutex.Lock()
	defer ism.mutex.Unlock()
	ism.state = state
}

// SetHistoryState sets the history navigation state
func (ism *InputStateManager) SetHistoryState(historyIndex int, tempInput []rune) {
	ism.mutex.Lock()
	defer ism.mutex.Unlock()
	ism.state.HistoryIndex = historyIndex
	ism.state.TempInput = string(tempInput)
}

// LoadFromFile loads input state from a file
func (ism *InputStateManager) LoadFromFile() error {
	if ism.file == "" {
		return nil
	}

	data, err := os.ReadFile(ism.file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file yet
		}
		return err
	}

	var state InputState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	ism.mutex.Lock()
	ism.state = state
	ism.mutex.Unlock()

	return nil
}

// SaveToFile saves input state to a file
func (ism *InputStateManager) SaveToFile() error {
	if ism.file == "" {
		return nil
	}

	ism.mutex.RLock()
	data, err := json.MarshalIndent(ism.state, "", "  ")
	ism.mutex.RUnlock()
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(ism.file)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(ism.file, data, 0600)
}

// ClearState clears the input state (resets to default)
func (ism *InputStateManager) ClearState() {
	ism.mutex.Lock()
	defer ism.mutex.Unlock()
	ism.state = InputState{
		HistoryIndex: -1,
		TempInput:    "",
	}
}
