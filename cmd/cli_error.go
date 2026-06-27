//go:build !js

package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// errReported marks an error whose user-facing message has already been
// rendered to the terminal by the command itself (e.g. the agent direct-mode
// path prints a friendly "✗ <message>" while streaming). Execute() recognizes
// it and skips the central re-print, so the user sees the failure once instead
// of twice — once nicely, once as a raw wrapped Go error chain.
var errReported = errors.New("error already reported to the user")

// markReported wraps cause so callers up the stack still see it (and the
// process still exits non-zero) while signalling that it was already shown.
func markReported(cause error) error {
	if cause == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", errReported, cause)
}

// plumbingPrefixes are internal "context" wrappers we add as an error bubbles
// up the command stack. They carry no meaning for a user, so the central
// renderer strips them from the front of the message — turning
// "failed to run direct mode: agent processing failed: chat failed: <cause>"
// into just "<cause>". We strip leading prefixes only (not an unconditional
// unwrap-to-leaf), so genuinely useful wrappers — e.g. "no provider
// configured. <hint>: <low-level cause>" — keep their guidance.
var plumbingPrefixes = []string{
	"failed to create chat agent: ",
	"failed to run direct mode: ",
	"agent processing failed: ",
	"chat failed: ",
}

// renderExecuteError is the single place the CLI turns a returned error into
// user-facing output. It is called once, from Execute(), with SilenceErrors /
// SilenceUsage set on the root command so cobra neither double-prints the raw
// error nor dumps the full flag list on a runtime failure.
//
// Already-reported errors produce no output (the command showed them).
// Provider-not-configured failures get a concise setup block. Everything else
// renders as a single "✗ <message>" line with internal plumbing prefixes
// stripped.
func renderExecuteError(err error) {
	if err == nil || errors.Is(err, errReported) {
		return
	}

	// --why flag: print the risk assessment for security errors
	if whyFlag {
		var agentErr *agenterrors.AgentError
		if errors.As(err, &agentErr) && agentErr.Category == agenterrors.CategorySecurity {
			why := agentErr.Why()
			if why != "" {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "  Risk assessment:")
				fmt.Fprintf(os.Stderr, "    %s\n", why)
			}
		}
	}

	// Provider-config / non-interactive failures: show the specific cause
	// (e.g. "HTTP 404: model not found", "Unknown Model") as the headline, then
	// a concise setup block. We deliberately don't replace the cause with a
	// generic "no provider" message — the hint text is appended to every
	// non-interactive provider failure, so it can't tell "nothing configured"
	// apart from "the configured provider rejected this model".
	if noninteractive.IsNonInteractiveHint(err) {
		console.GlyphError.Fprintln(os.Stderr, leafErrorMessage(err))
		renderProviderSetupHint()
		return
	}
	console.GlyphError.Fprintln(os.Stderr, cleanErrorMessage(err))
}

// renderProviderSetupHint prints an actionable block on how to configure a
// provider, shown after a provider-resolution failure in non-interactive mode.
func renderProviderSetupHint() {
	fmt.Fprintln(os.Stderr, "  Configure a provider with any of:")
	fmt.Fprintln(os.Stderr, "    sprout custom add               # add an OpenAI-compatible provider")
	fmt.Fprintln(os.Stderr, "    sprout keys set <provider>      # store & validate an API key for a built-in provider")
	fmt.Fprintln(os.Stderr, "    export <PROVIDER>_API_KEY=...   # set via environment variable")
	fmt.Fprintln(os.Stderr, "    export SPROUT_PROVIDER=<name>   # select an already-configured provider")
	fmt.Fprintln(os.Stderr, "  Or run 'sprout agent' interactively (without piping input) for guided setup.")
}

// leafErrorMessage walks to the deepest wrapped error and returns its message —
// the actual low-level cause (an HTTP status, a model-not-found), used as the
// headline for provider failures where the wrapper is just boilerplate.
func leafErrorMessage(err error) string {
	leaf := err
	for {
		unwrapped := errors.Unwrap(leaf)
		if unwrapped == nil {
			break
		}
		leaf = unwrapped
	}
	if msg := strings.TrimSpace(leaf.Error()); msg != "" {
		return msg
	}
	return strings.TrimSpace(err.Error())
}

// cleanErrorMessage strips leading internal plumbing prefixes from the error
// message so the user sees the meaningful cause rather than our call-stack.
func cleanErrorMessage(err error) string {
	msg := strings.TrimSpace(err.Error())
	for changed := true; changed; {
		changed = false
		for _, p := range plumbingPrefixes {
			if stripped, ok := strings.CutPrefix(msg, p); ok {
				msg = strings.TrimSpace(stripped)
				changed = true
			}
		}
	}
	return msg
}
