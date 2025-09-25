package console

import (
	"fmt"
	"os"
)

// DebugEnabled returns true if debug output is enabled
func DebugEnabled() bool {
	return os.Getenv("LEDIT_DEBUG") != ""
}

// DebugPrintf prints debug output to stderr if debugging is enabled
func DebugPrintf(format string, args ...interface{}) {
	if DebugEnabled() {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format, args...)
	}
}
