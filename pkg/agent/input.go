package agent

import (
	"fmt"
)

// InjectInputContext injects a new user input using context-based interrupt system
func (a *Agent) InjectInputContext(input string) error {
	a.inputInjectionMutex.Lock()
	defer a.inputInjectionMutex.Unlock()

	// Send the new input to the injection channel
	select {
	case a.inputInjectionChan <- input:
		return nil
	default:
		return fmt.Errorf("input injection channel full")
	}
}

// GetInputInjectionContext returns the input injection channel for the new system
func (a *Agent) GetInputInjectionContext() <-chan string {
	return a.inputInjectionChan
}

// ClearInputInjectionContext clears any pending input injections
func (a *Agent) ClearInputInjectionContext() {
	a.inputInjectionMutex.Lock()
	defer a.inputInjectionMutex.Unlock()

	// Drain the channel
	for {
		select {
		case <-a.inputInjectionChan:
			// Remove item
		default:
			// Channel empty
			return
		}
	}
}

// IsInterrupted returns true if an interrupt has been requested
func (a *Agent) IsInterrupted() bool {
	return a.CheckForInterrupt()
}
