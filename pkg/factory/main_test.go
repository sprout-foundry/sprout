package factory

import (
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

// Custom providers are saved to the user-global config dir; keep this
// package's tests out of the developer's real one.
func TestMain(m *testing.M) {
	finish := configuration.IsolateGlobalConfigForTests()
	// CreateProviderClient(SproutLocalClientType) returns the process-wide
	// local provider, whose first use loads the developer's real multi-GB
	// model onto the GPU — measured at 14.4GB active and 32s for a test
	// that only asserts GetProvider() and SupportsVision(). Pin it to the
	// inert backend, as pkg/webui does for the same reason. The loading
	// paths themselves are covered by pkg/localmodel's own tests.
	localmodel.DisableForTesting()
	os.Exit(finish(m.Run()))
}
