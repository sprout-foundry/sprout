package interactive

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// TerminalInput provides raw terminal input with paste detection
type TerminalInput struct {
	oldState *term.State
	prompt   string
}

// NewTerminalInput creates a new terminal input handler
func NewTerminalInput(prompt string) *TerminalInput {
	return &TerminalInput{
		prompt: prompt,
	}
}

// ReadLineWithPasteDetection reads input and detects if it was pasted
func (t *TerminalInput) ReadLineWithPasteDetection() (string, bool, error) {
	// Set raw mode
	var err error
	t.oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", false, err
	}
	defer term.Restore(int(os.Stdin.Fd()), t.oldState)

	fmt.Print(t.prompt)

	var buffer []byte
	var lines []string
	isPaste := false
	lastReadTime := time.Now()

	// Read with larger buffer to catch pastes
	readBuf := make([]byte, 4096)

	for {
		// Set read timeout to detect paste gaps
		n, err := os.Stdin.Read(readBuf)
		if err != nil {
			return "", false, err
		}

		// Detect paste: got multiple bytes at once
		if n > 1 {
			isPaste = true
		}

		// Process bytes
		for i := 0; i < n; i++ {
			b := readBuf[i]

			switch b {
			case 3: // Ctrl+C
				fmt.Println("\n^C")
				return "", false, fmt.Errorf("interrupted")

			case 4: // Ctrl+D (EOF)
				fmt.Println()
				if isPaste && len(buffer) > 0 {
					// Add last line
					lines = append(lines, string(buffer))
				}
				result := strings.Join(lines, "\n")
				return result, isPaste, nil

			case 13, 10: // Enter
				if isPaste {
					// In paste mode, collect lines
					lines = append(lines, string(buffer))
					buffer = buffer[:0]

					// Check if more data is coming
					time.Sleep(10 * time.Millisecond)
					// Try non-blocking read to see if more paste data
					// This is a bit hacky but works for paste detection
					continue
				} else {
					// Normal mode - return single line
					fmt.Println()
					return string(buffer), false, nil
				}

			case 127, 8: // Backspace
				if !isPaste && len(buffer) > 0 {
					buffer = buffer[:len(buffer)-1]
					fmt.Print("\b \b")
				}

			default:
				// Regular character
				if b >= 32 && b < 127 {
					buffer = append(buffer, b)
					if !isPaste {
						fmt.Print(string(b))
					}
				}
			}
		}

		// Check time since last read
		now := time.Now()
		if isPaste && now.Sub(lastReadTime) > 50*time.Millisecond {
			// Paste finished - join all content
			if len(buffer) > 0 {
				lines = append(lines, string(buffer))
			}
			fmt.Println("\n[Paste detected]")
			result := strings.Join(lines, "\n")
			return result, true, nil
		}
		lastReadTime = now
	}
}
