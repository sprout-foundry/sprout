package interactive

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// RawInput handles raw terminal input with paste detection
type RawInput struct {
	oldState *term.State
	buffer   []byte
	pos      int
}

// NewRawInput creates a new raw input handler
func NewRawInput() (*RawInput, error) {
	// Save current terminal state
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}

	return &RawInput{
		oldState: oldState,
		buffer:   make([]byte, 0),
		pos:      0,
	}, nil
}

// Close restores terminal to original state
func (r *RawInput) Close() error {
	return term.Restore(int(os.Stdin.Fd()), r.oldState)
}

// ReadLine reads a line with paste detection
func (r *RawInput) ReadLine(prompt string) (string, bool, error) {
	fmt.Print(prompt)

	r.buffer = r.buffer[:0]
	r.pos = 0

	readBuf := make([]byte, 1024)
	isPaste := false
	lastReadTime := time.Now()

	for {
		n, err := os.Stdin.Read(readBuf)
		if err != nil {
			return "", false, err
		}

		// Detect paste: multiple bytes at once or rapid succession
		now := time.Now()
		if n > 1 || now.Sub(lastReadTime) < 10*time.Millisecond {
			isPaste = true
		}
		lastReadTime = now

		for i := 0; i < n; i++ {
			b := readBuf[i]

			switch b {
			case 3: // Ctrl+C
				fmt.Println("^C")
				return "", false, fmt.Errorf("interrupted")

			case 4: // Ctrl+D
				if len(r.buffer) == 0 {
					return "", false, fmt.Errorf("EOF")
				}

			case 13, 10: // Enter
				fmt.Println()
				return string(r.buffer), isPaste, nil

			case 127, 8: // Backspace
				if r.pos > 0 {
					r.buffer = append(r.buffer[:r.pos-1], r.buffer[r.pos:]...)
					r.pos--
					// Move cursor back, print space, move back again
					fmt.Print("\b \b")
				}

			case 27: // Escape sequences (arrows, etc)
				// Skip next 2 bytes for arrow keys
				if i+2 < n && readBuf[i+1] == 91 {
					switch readBuf[i+2] {
					case 68: // Left arrow
						if r.pos > 0 {
							r.pos--
							fmt.Print("\b")
						}
					case 67: // Right arrow
						if r.pos < len(r.buffer) {
							r.pos++
							fmt.Print(string(r.buffer[r.pos-1]))
						}
					}
					i += 2
				}

			default:
				// Regular character
				if b >= 32 && b < 127 { // Printable ASCII
					r.buffer = insert(r.buffer, r.pos, b)
					r.pos++

					// Reprint from cursor position
					fmt.Print(string(r.buffer[r.pos-1:]))
					// Move cursor back to correct position
					for j := r.pos; j < len(r.buffer); j++ {
						fmt.Print("\b")
					}
				}
			}
		}
	}
}

// insert inserts a byte at position
func insert(slice []byte, pos int, value byte) []byte {
	if pos == len(slice) {
		return append(slice, value)
	}
	slice = append(slice[:pos+1], slice[pos:]...)
	slice[pos] = value
	return slice
}

// ReadMultiLine reads until EOF with paste detection
func (r *RawInput) ReadMultiLine(prompt string) (string, error) {
	fmt.Println(prompt)

	var lines []string

	for {
		line, _, err := r.ReadLine("")
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return "", err
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n"), nil
}
