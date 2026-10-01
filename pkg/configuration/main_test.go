package configuration

import (
	"os"
	"testing"
)

// Custom providers are saved to the user-global config dir; keep this
// package's tests out of the developer's real one.
func TestMain(m *testing.M) {
	finish := IsolateGlobalConfigForTests()
	os.Exit(finish(m.Run()))
}
