package interactive

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// DebugInput is a minimal test to isolate the performance issue
func DebugInput() {
	fmt.Println("Debug input test - type characters and watch for slowdown")
	fmt.Println("Press Ctrl+C to exit")
	fmt.Println("----------------------------------------")

	// Get terminal width
	width := 80 // default
	if w := os.Getenv("COLUMNS"); w != "" {
		fmt.Sscanf(w, "%d", &width)
	}
	fmt.Printf("Terminal width: %d\n", width)

	fmt.Print("> ")

	// Put terminal in raw mode
	oldState, _ := term.MakeRaw(int(os.Stdin.Fd()))
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	charCount := 0
	lineStart := time.Now()
	lastChar := time.Now()

	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		// Measure read time
		readTime := time.Since(lastChar)

		switch buf[0] {
		case 3: // Ctrl+C
			fmt.Println("\nExiting...")
			return

		case 13, 10: // Enter
			fmt.Printf("\nTotal chars: %d, Total time: %v, Avg: %v/char\n",
				charCount,
				time.Since(lineStart),
				time.Since(lineStart)/time.Duration(charCount))
			fmt.Print("> ")
			charCount = 0
			lineStart = time.Now()

		default:
			if buf[0] >= 32 {
				charCount++

				// Time the output
				outStart := time.Now()
				fmt.Printf("%c", buf[0])
				outTime := time.Since(outStart)

				// Report if slow
				if readTime > 10*time.Millisecond || outTime > 5*time.Millisecond {
					fmt.Fprintf(os.Stderr, "\n[SLOW] Char %d: read=%v, output=%v\n",
						charCount, readTime, outTime)
				}

				// Check if we're near line wrap
				if charCount > 0 && charCount%10 == 0 {
					fmt.Fprintf(os.Stderr, "\n[INFO] Char count: %d\n", charCount)
					fmt.Print("> ") // Re-show prompt
					// Reprint the line so far
					for i := 0; i < charCount; i++ {
						fmt.Print("x")
					}
				}
			}
		}

		lastChar = time.Now()
	}
}
