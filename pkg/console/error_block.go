package console

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// errorBlockRedFG is the ANSI escape used to color a multi-line error block
// when the user hasn't opted out via NO_COLOR. Reset (\033[0m) restores
// terminal defaults rather than the footer's cyan, because error blocks
// live in the scrolled region, not the footer chrome.
const errorBlockRedFG = "\033[31m"

// FormatErrorBlock turns an error into a CLI-renderable block.
//
//   - nil error → empty string. Callers can append it directly to a header
//     line without an extra branch.
//   - Single-line error → "<header>: <err>\n", matching today's
//     `fmt.Fprintf(os.Stderr, "[FAIL] Error: %v\n", err)` output exactly so
//     existing logs aren't disturbed.
//   - Multi-line error (contains \n after trimming the trailing newline)
//     → "<header>:\n  <line1>\n  <line2>\n…", indented by two spaces, with
//     red coloring when ANSI colors are enabled. Preserves the full stderr
//     a tool produced instead of collapsing it to its first line.
//
// The two-space indent matches the rest of sprout's CLI conventions (tool
// timeline, footer padding) so a multi-line error reads as part of the
// same chrome rather than a foreign block.
func FormatErrorBlock(header string, err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimRight(err.Error(), "\n")
	if !strings.Contains(msg, "\n") {
		return fmt.Sprintf("%s: %s\n", header, msg)
	}

	useColor := envutil.ResolveColorPreference(true)
	lines := strings.Split(msg, "\n")
	var b strings.Builder
	b.WriteString(header)
	b.WriteString(":\n")
	for _, line := range lines {
		if useColor {
			b.WriteString(errorBlockRedFG)
		}
		b.WriteString("  ")
		b.WriteString(line)
		if useColor {
			b.WriteString("\033[0m")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
