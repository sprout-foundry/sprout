package components

import (
	"os"
	"strings"
	"sync"

	"github.com/alantheprice/ledit/pkg/filesystem"
)

// HistoryManager manages command history with persistence
type HistoryManager struct {
	mutex    sync.RWMutex
	history  []string
	maxSize  int
	filename string
}

// NewHistoryManager creates a new history manager
func NewHistoryManager(filename string, maxSize int) *HistoryManager {
	if maxSize <= 0 {
		maxSize = 1000 // Default max history size
	}

	return &HistoryManager{
		history:  make([]string, 0, maxSize),
		maxSize:  maxSize,
		filename: filename,
	}
}

// AddEntry adds a new entry to history
func (hm *HistoryManager) AddEntry(entry string) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return
	}

	hm.mutex.Lock()
	defer hm.mutex.Unlock()

	// Remove duplicate if it exists
	for i, existing := range hm.history {
		if existing == entry {
			hm.history = append(hm.history[:i], hm.history[i+1:]...)
			break
		}
	}

	// Add to end
	hm.history = append(hm.history, entry)

	// Trim if too large
	if len(hm.history) > hm.maxSize {
		hm.history = hm.history[len(hm.history)-hm.maxSize:]
	}
}

// GetHistory returns a copy of the history
func (hm *HistoryManager) GetHistory() []string {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()

	result := make([]string, len(hm.history))
	copy(result, hm.history)
	return result
}

// GetEntry returns a specific history entry by index (0 = oldest)
func (hm *HistoryManager) GetEntry(index int) (string, bool) {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()

	if index < 0 || index >= len(hm.history) {
		return "", false
	}

	return hm.history[index], true
}

// GetLatest returns the most recent entries (newest first)
func (hm *HistoryManager) GetLatest(count int) []string {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()

	if count <= 0 || len(hm.history) == 0 {
		return nil
	}

	start := len(hm.history) - count
	if start < 0 {
		start = 0
	}

	// Return in reverse order (newest first)
	result := make([]string, len(hm.history)-start)
	for i, j := len(hm.history)-1, 0; i >= start; i, j = i-1, j+1 {
		result[j] = hm.history[i]
	}

	return result
}

// Size returns the number of history entries
func (hm *HistoryManager) Size() int {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()
	return len(hm.history)
}

// Clear removes all history entries
func (hm *HistoryManager) Clear() {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()
	hm.history = hm.history[:0]
}

// LoadFromFile loads history from a file
func (hm *HistoryManager) LoadFromFile() error {
	if hm.filename == "" {
		return nil
	}

	data, err := filesystem.ReadFileBytes(hm.filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No history file yet
		}
		return err
	}

	lines := strings.Split(string(data), "\n")

	hm.mutex.Lock()
	defer hm.mutex.Unlock()

	hm.history = hm.history[:0] // Clear existing
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			hm.history = append(hm.history, line)
		}
	}

	// Trim if loaded history is too large
	if len(hm.history) > hm.maxSize {
		hm.history = hm.history[len(hm.history)-hm.maxSize:]
	}

	return nil
}

// SaveToFile saves history to a file
func (hm *HistoryManager) SaveToFile() error {
	if hm.filename == "" {
		return nil
	}

	hm.mutex.RLock()
	data := strings.Join(hm.history, "\n")
	hm.mutex.RUnlock()

	return filesystem.WriteFileWithDir(hm.filename, []byte(data), 0600)
}
