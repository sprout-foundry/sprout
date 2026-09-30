package providers_test

import (
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// Custom providers are saved to the user-global config dir; keep this
// package's tests out of the developer's real one.
func TestMain(m *testing.M) {
	finish := configuration.IsolateGlobalConfigForTests()
	os.Exit(finish(m.Run()))
}
