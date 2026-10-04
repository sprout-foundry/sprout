//go:build !js

// agent_verification_exit.go — the SP-149 §149e CLI exit contract: a
// non-interactive `sprout agent` run exits non-zero when verification
// is enabled and fails. The turn-end hook (items 149.5/149.6) already
// stores the turn's verification result on the agent
// (Agent.LastVerificationResult); this file is where the cmd layer
// turns it into an exit code. The exit code is a CLI concern — the
// agent returns (result, nil) for a turn that COMPLETED with a failing
// verification result, so the gate lives here, at the direct-mode
// completion points (RunAgent), never in pkg/agent.
//
// Behavior-preserving by construction: verification disabled (the
// default), a turn that changed no code, a subagent turn, or a runner
// setup error all leave the stored result nil, so every existing code
// path and output is unchanged when verification is off (§149e:
// disabled verification changes no behavior).

package cmd

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// verificationExitError is the pure core of the §149e exit contract:
// it maps a turn's stored verification result to the error a
// direct-mode completion point should return.
//
//   - nil result → nil: verification never ran (disabled, no code
//     change, subagent, setup error) — behavior is unchanged.
//   - passing result (res.Passed()) → nil.
//   - all-skipped result (verified nothing: neither Passed() nor
//     Failed()) → nil: §149e gates on a verification FAILURE; a run
//     that verified nothing is not a failure.
//   - failing result (res.Failed()) → a plain error carrying the run's
//     summary. A plain error maps to exit code 1 via exitCodeFor and
//     renders as one clean line via renderExecuteError.
func verificationExitError(res *verify.Result) error {
	if res == nil || !res.Failed() {
		return nil
	}
	return fmt.Errorf("verification failed: %s", res.Summary())
}

// verificationRunExitError is the agent-facing wrapper used at the two
// direct-mode completion points in RunAgent: nil-safe (a nil agent
// never gates the exit), it reads the agent's last turn-end
// verification result and delegates to verificationExitError.
func verificationRunExitError(chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return nil
	}
	return verificationExitError(chatAgent.LastVerificationResult())
}
