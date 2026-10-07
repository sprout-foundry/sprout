package verify

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ConfigurationCommands returns a ConfigCommandsProvider backed by a
// merged *configuration.Config — the "explicit project configuration"
// source of the explicit project configuration: verification.build_command and
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

// QualityConfigurationCommands returns a QualityConfigCommandsProvider backed
// by a merged *configuration.Config — the "explicit project configuration"
// source of the quality-after-edits step: quality.format_command and
// quality.lint_command, set by a human in the project layer
// (.sprout/workspace.json) or in global config. The provider ignores root:
// the config is already the merged result for the project.
func QualityConfigurationCommands(cfg *configuration.Config) QualityConfigCommandsProvider {
	return func(string) (QualityCommands, error) {
		return QualityCommands{
			Format: strings.TrimSpace(cfg.QualityFormatCommand()),
			Lint:   strings.TrimSpace(cfg.QualityLintCommand()),
		}, nil
	}
}
