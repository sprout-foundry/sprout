package console

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LogInputHandler handles input for the interactive log command
type LogInputHandler struct {
	handlerID     string
	inputChan     chan string
	interruptChan chan bool
	isActive      bool
}

// NewLogInputHandler creates a new log input handler
func NewLogInputHandler() *LogInputHandler {
	return &LogInputHandler{
		handlerID:     "log_navigator",
		inputChan:     make(chan string, 10),
		interruptChan: make(chan bool, 1),
		isActive:      false,
	}
}

// GetHandlerID returns the handler identifier
func (lih *LogInputHandler) GetHandlerID() string {
	return lih.handlerID
}

// HandleInput processes input events for log navigation
func (lih *LogInputHandler) HandleInput(event InputEvent) bool {
	if !lih.isActive {
		return false
	}

	switch event.Type {
	case KeystrokeEvent:
		if data, ok := event.Data.(KeystrokeData); ok {
			// Convert keystrokes to string input
			input := strings.TrimSpace(string(data.Bytes))

			// Handle special keys
			switch {
			case len(data.Bytes) == 1 && data.Bytes[0] == 13: // Enter key
				select {
				case lih.inputChan <- "":
				default:
				}
			case len(data.Bytes) == 1 && data.Bytes[0] == 3: // Ctrl+C
				select {
				case lih.interruptChan <- true:
				default:
				}
			case len(data.Bytes) == 1:
				// Regular character input
				char := string(data.Bytes[0])
				select {
				case lih.inputChan <- char:
				default:
				}
			case len(data.Bytes) == 3 && data.Bytes[0] == 27 && data.Bytes[1] == '[':
				// Arrow keys
				switch data.Bytes[2] {
				case 'A': // Up arrow
					select {
					case lih.inputChan <- "":
					default:
					}
				case 'B': // Down arrow - treat as next
					select {
					case lih.inputChan <- "":
					default:
					}
				}
			default:
				// Convert to string for other inputs
				if input != "" {
					select {
					case lih.inputChan <- input:
					default:
					}
				}
			}
			return true
		}
	case InterruptEvent:
		select {
		case lih.interruptChan <- true:
		default:
		}
		return true
	}

	return false
}

// Activate enables this handler to receive input
func (lih *LogInputHandler) Activate() {
	lih.isActive = true
}

// Deactivate disables this handler from receiving input
func (lih *LogInputHandler) Deactivate() {
	lih.isActive = false
}

// ReadString mimics bufio.Reader.ReadString for compatibility with existing log code
func (lih *LogInputHandler) ReadString(delim byte) (string, error) {
	if !lih.isActive {
		return "", fmt.Errorf("handler not active")
	}

	select {
	case input := <-lih.inputChan:
		return input + string(delim), nil
	case <-lih.interruptChan:
		return "", fmt.Errorf("interrupted")
	}
}

// CreateReader creates a bufio.Reader-compatible interface for the log handler
func (lih *LogInputHandler) CreateReader() *bufio.Reader {
	// Create a pipe that we can write to from our input handler
	r, w, err := os.Pipe()
	if err != nil {
		return nil
	}

	// Start a goroutine that feeds input from our handler to the pipe
	go func() {
		defer w.Close()
		for lih.isActive {
			select {
			case input := <-lih.inputChan:
				fmt.Fprintf(w, "%s\n", input)
			case <-lih.interruptChan:
				return
			}
		}
	}()

	return bufio.NewReader(r)
}
