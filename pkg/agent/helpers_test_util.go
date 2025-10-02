package agent

import (
	"os"
	"strings"
)

// isRunningUnderTest returns true if running under `go test` by checking
// common signs: -test.* flags in os.Args or the program name ending with ".test".
func isRunningUnderTest() bool {
	// Explicit env override
	if os.Getenv("LEDIT_TEST_ENV") == "1" {
		return true
	}

	// Check for test flags passed by `go test`
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") {
			return true
		}
	}

	// Some runners set the executable name to end with .test
	if strings.HasSuffix(os.Args[0], ".test") {
		return true
	}

	return false
}
