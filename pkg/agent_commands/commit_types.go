package commands

import (
	"errors"
	"github.com/sprout-foundry/sprout/pkg/configuration"

	gitops "github.com/sprout-foundry/sprout/pkg/git"
)

// Commit status constants
const (
	CommitStatusSuccess = "success"
	CommitStatusError   = "error"
	CommitStatusDryRun  = "dry-run"
)

// CommitCommand implements the /commit slash command
type CommitCommand struct {
	skipPrompt       bool
	dryRun           bool
	allowSecrets     bool
	agentError       error  // Store agent creation error for better error reporting
	review           string // Store the commit review result
	userInstructions string // User-provided instructions for the commit message
	// trailers are explicit git trailers (--trailer "Key: Value") appended
	// to the commit message in addition to the commit.trailers defaults.
	trailers []string
	// providerOverride and modelOverride are the --provider/--model flags:
	// when set they win over the commit role in the config.
	providerOverride string
	modelOverride    string
}

// SetModelOverride sets the provider and model the commit message is
// generated with (the --provider/--model flags), over the commit role in the
// config. An empty provider keeps the configured one with the given model.
func (c *CommitCommand) SetModelOverride(provider, model string) {
	c.providerOverride = provider
	c.modelOverride = model
}

// commitProviderModel is the provider and model commit messages are
// generated with: the flags when given, else the commit role.
func (c *CommitCommand) commitProviderModel(cfg *configuration.Config) (provider, model string) {
	if cfg != nil {
		provider, model = cfg.ResolveRole(configuration.RoleCommit)
	}
	switch {
	case c.providerOverride != "":
		provider, model = c.providerOverride, c.modelOverride
	case c.modelOverride != "":
		model = c.modelOverride
	}
	return provider, model
}

// CommitJSONResult is the JSON output structure for commit command
type CommitJSONResult struct {
	Status  string `json:"status"`            // success, error, dry-run
	Commit  string `json:"commit,omitempty"`  // commit hash if successful
	Message string `json:"message,omitempty"` // commit message
	Branch  string `json:"branch,omitempty"`  // branch name
	Error   string `json:"error,omitempty"`   // error message
	Review  string `json:"review,omitempty"`  // commit review (critical concerns)
}

// Validate checks that required fields are populated
func (r *CommitJSONResult) Validate() error {
	if r.Status == "" {
		return errors.New("status field is required")
	}
	switch r.Status {
	case CommitStatusSuccess:
		if r.Commit == "" {
			return errors.New("commit hash required for success status")
		}
	case CommitStatusError:
		if r.Error == "" {
			return errors.New("error message required for error status")
		}
	case CommitStatusDryRun:
		// Only status required for dry-run
	}
	return nil
}

// wrapText wraps text to a specific line length
func wrapText(text string, lineLength int) string {
	return gitops.WrapText(text, lineLength)
}
