package verify

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ConfigurationCommands returns a ConfigCommandsProvider backed by a
// merged *configuration.Config — the "explicit project configuration"
// source of SP-149 §149b: verification.build_command and
// verification.test_command, set by a human in the project layer
// (.sprout/workspace.json) or in global config. The provider ignores
// root: the config is already the merged result for the project.
func ConfigurationCommands(cfg *configuration.Config) ConfigCommandsProvider {
	return func(string) (Commands, error) {
		return Commands{
			Build: strings.TrimSpace(cfg.VerificationBuildCommand()),
			Test:  strings.TrimSpace(cfg.VerificationTestCommand()),
		}, nil
	}
}
